package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// MaxPendingPairings is the number of live, unused pairing codes one tenant may have.
	MaxPendingPairings = 5
	// MaxDevices is the number of non-revoked devices one tenant may have.
	MaxDevices = 50
	// DefaultPairingTTL is the life of a pairing code when the caller gives none.
	DefaultPairingTTL = 10 * time.Minute
)

var (
	// ErrPairingCodeInvalid: the code is unknown, used, cancelled or expired. One error for
	// all of them, so the caller cannot tell a guess from a spent code.
	ErrPairingCodeInvalid = errors.New("invalid pairing code")
	// ErrPendingLimit: the tenant already has MaxPendingPairings live codes.
	ErrPendingLimit = errors.New("too many pending pairing codes")
	// ErrDeviceLimit: the tenant already has MaxDevices devices.
	ErrDeviceLimit = errors.New("too many devices")
)

// Pairing is one pairing code row; the code itself is not stored.
type Pairing struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Label     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Device is one paired phone. The key is not stored, only its hash.
type Device struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Label      string
	Operator   string
	AppVersion string
	PairingID  *uuid.UUID
	CreatedAt  time.Time
	LastSeenAt *time.Time
	RevokedAt  *time.Time
}

// DeviceInfo is what the phone tells at exchange.
type DeviceInfo struct {
	Label      string
	Operator   string
	AppVersion string
}

const deviceColumns = `device_id, tenant_id, label, operator, app_version, pairing_id, created_at, last_seen_at, revoked_at`

func scanDevice(row pgx.Row) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.TenantID, &d.Label, &d.Operator, &d.AppVersion, &d.PairingID, &d.CreatedAt, &d.LastSeenAt, &d.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// lockTenant takes the tenant row lock, which serialises the limit checks of one tenant.
func lockTenant(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `select tenant_id from tenants where tenant_id = $1 for update`, tenantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// CreatePairing makes a pairing code for a tenant, valid for ttl from now (zero: the
// default). The clear code is returned once; only its hash is stored. At most
// MaxPendingPairings live codes per tenant (ErrPendingLimit).
func (s *Store) CreatePairing(ctx context.Context, tenantID uuid.UUID, label string, ttl time.Duration, now time.Time) (Pairing, string, error) {
	if ttl <= 0 {
		ttl = DefaultPairingTTL
	}
	if len([]rune(label)) > 200 {
		return Pairing{}, "", invalid("label is too long")
	}
	var p Pairing
	var code string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		var pending int
		if err := tx.QueryRow(ctx, `
			select count(*) from device_pairings
			where tenant_id = $1 and used_at is null and cancelled_at is null and expires_at > $2`,
			tenantID, now).Scan(&pending); err != nil {
			return err
		}
		if pending >= MaxPendingPairings {
			return ErrPendingLimit
		}
		var err error
		if code, err = NewPairingCode(); err != nil {
			return err
		}
		norm, _ := NormalizePairingCode(code)
		p = Pairing{ID: uuid.New(), TenantID: tenantID, Label: label, CreatedAt: now, ExpiresAt: now.Add(ttl)}
		_, err = tx.Exec(ctx, `
			insert into device_pairings (pairing_id, tenant_id, code_hash, label, created_at, expires_at)
			values ($1, $2, $3, $4, $5, $6)`,
			p.ID, tenantID, hashBytes(norm), label, p.CreatedAt, p.ExpiresAt)
		return err
	})
	if err != nil {
		return Pairing{}, "", err
	}
	return p, code, nil
}

// CancelPairing cancels a live code of the tenant. A code of another tenant, an unknown
// one, a used or an already cancelled one is ErrNotFound.
func (s *Store) CancelPairing(ctx context.Context, tenantID, pairingID uuid.UUID, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		update device_pairings set cancelled_at = $3
		where pairing_id = $1 and tenant_id = $2 and used_at is null and cancelled_at is null`,
		pairingID, tenantID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ExchangePairing spends a pairing code and creates the device, in one transaction. The
// update of the code row has one winner when several phones race for it; the others get
// ErrPairingCodeInvalid. When the device limit is hit the transaction rolls back and the
// code stays usable. The device key is returned once; only its hash is stored.
func (s *Store) ExchangePairing(ctx context.Context, rawCode string, info DeviceInfo, now time.Time) (Device, string, error) {
	code, ok := NormalizePairingCode(rawCode)
	if !ok {
		return Device{}, "", ErrPairingCodeInvalid
	}
	if len([]rune(info.Label)) > 200 || len([]rune(info.Operator)) > 200 || len(info.AppVersion) > 50 {
		return Device{}, "", invalid("device label, operator or app version is too long")
	}
	var d Device
	var key string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var pairingID, tenantID uuid.UUID
		var pLabel string
		err := tx.QueryRow(ctx, `
			update device_pairings set used_at = $2
			where code_hash = $1 and used_at is null and cancelled_at is null and expires_at > $2
			returning pairing_id, tenant_id, label`, hashBytes(code), now).Scan(&pairingID, &tenantID, &pLabel)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPairingCodeInvalid
		}
		if err != nil {
			return err
		}
		if err := lockTenant(ctx, tx, tenantID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `select count(*) from devices where tenant_id = $1 and revoked_at is null`, tenantID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxDevices {
			return ErrDeviceLimit
		}
		if key, err = NewDeviceKey(); err != nil {
			return err
		}
		label := info.Label
		if label == "" {
			label = pLabel
		}
		d, err = scanDevice(tx.QueryRow(ctx, `
			insert into devices (device_id, tenant_id, key_hash, label, operator, app_version, pairing_id, created_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8) returning `+deviceColumns,
			uuid.New(), tenantID, hashBytes(key), label, info.Operator, info.AppVersion, pairingID, now))
		return err
	})
	if err != nil {
		return Device{}, "", err
	}
	return d, key, nil
}

// DeviceByKey finds the device that owns a key ("qdk_..."), revoked ones included, or
// ErrNotFound. The caller decides what a revoked device may do.
func (s *Store) DeviceByKey(ctx context.Context, key string) (Device, error) {
	return scanDevice(s.pool.QueryRow(ctx, `select `+deviceColumns+` from devices where key_hash = $1`, hashBytes(key)))
}

// ListDevices returns the tenant's devices, newest first, revoked ones included.
func (s *Store) ListDevices(ctx context.Context, tenantID uuid.UUID) ([]Device, error) {
	rows, err := s.pool.Query(ctx, `select `+deviceColumns+` from devices where tenant_id = $1 order by created_at desc, device_id`, tenantID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) { return scanDevice(r) })
}

// RevokeDevice marks a device of the tenant revoked; the row is kept. Revoking twice keeps
// the first time. A device of another tenant or an unknown one is ErrNotFound.
func (s *Store) RevokeDevice(ctx context.Context, tenantID, deviceID uuid.UUID, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		update devices set revoked_at = coalesce(revoked_at, $3)
		where device_id = $1 and tenant_id = $2`, deviceID, tenantID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchDevice records that the device was seen at now, and its app version when given.
// It writes only when the last mark is older than minAge, so a busy phone does not write
// on every request. A revoked device is not touched.
func (s *Store) TouchDevice(ctx context.Context, deviceID uuid.UUID, appVersion string, now time.Time, minAge time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		update devices set last_seen_at = $2, app_version = case when $3 = '' then app_version else $3 end
		where device_id = $1 and revoked_at is null
		  and (last_seen_at is null or last_seen_at <= $2::timestamptz - $4::float8 * interval '1 second')`,
		deviceID, now, appVersion, minAge.Seconds())
	return err
}
