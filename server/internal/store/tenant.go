package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TenantActive    = "active"
	TenantSuspended = "suspended"
	TenantExpired   = "expired"
)

// Tenant is one row of the registry. Its ledger lives in the schema SchemaName.
type Tenant struct {
	ID         uuid.UUID
	Name       string
	Language   string
	Tier       string
	OwnerEmail string
	SchemaName string
	Status     string
	CreatedAt  time.Time
	ExpiresAt  *time.Time // nil: never expires
}

// EffectiveStatus is the status as of now: a demo tenant past its expires_at is
// "expired" even though the row still says otherwise. The row is not rewritten.
func (t Tenant) EffectiveStatus(now time.Time) string {
	if t.ExpiresAt != nil && !t.ExpiresAt.After(now) {
		return TenantExpired
	}
	return t.Status
}

// TenantInput is what the provisioning API passes to CreateTenant.
type TenantInput struct {
	ID         string
	Name       string
	Language   string
	Tier       string // demo, production, or a stored tier value (legacy)
	Plan       string // own_keys or all_inclusive; used only with Tier "production"
	OwnerEmail string
	ExpiresAt  *time.Time // demo only; must be after Now
	Now        time.Time  // the clock for ExpiresAt; zero means time.Now()
}

var (
	tenantLanguages = map[string]bool{"ru": true, "kk": true, "en": true}
)

// StoredTier maps the API's tier and plan to the stored tier. The three stored values
// are accepted as they are and then plan is ignored; plan is also ignored with "demo".
// An empty plan with "production" means own_keys.
func StoredTier(tier, plan string) (string, error) {
	switch tier {
	case "demo", "hosted_own_keys", "hosted_all_inclusive":
		return tier, nil
	case "production":
		switch plan {
		case "", "own_keys":
			return "hosted_own_keys", nil
		case "all_inclusive":
			return "hosted_all_inclusive", nil
		}
		return "", invalid("plan must be own_keys or all_inclusive")
	}
	return "", invalid("tier must be demo or production")
}

// TierAndPlan is the inverse of StoredTier: the API's tier and plan ("" for no plan).
func TierAndPlan(stored string) (tier, plan string) {
	switch stored {
	case "hosted_own_keys":
		return "production", "own_keys"
	case "hosted_all_inclusive":
		return "production", "all_inclusive"
	}
	return stored, ""
}

func (in TenantInput) validate() (uuid.UUID, error) {
	id, err := uuid.Parse(in.ID)
	if err != nil {
		return uuid.Nil, invalid("tenant_id must be a UUID")
	}
	switch {
	case len([]rune(strings.TrimSpace(in.Name))) < 1 || len([]rune(in.Name)) > 200:
		return uuid.Nil, invalid("name must be 1 to 200 characters")
	case !tenantLanguages[in.Language]:
		return uuid.Nil, invalid("language must be ru, kk or en")
	case len(in.OwnerEmail) > 254:
		return uuid.Nil, invalid("owner_email is too long")
	}
	if _, err := StoredTier(in.Tier, in.Plan); err != nil {
		return uuid.Nil, err
	}
	if in.ExpiresAt != nil {
		now := in.Now
		if now.IsZero() {
			now = time.Now()
		}
		switch {
		case in.Tier != "demo":
			return uuid.Nil, invalid("expires_at is allowed only with tier demo")
		case !in.ExpiresAt.After(now):
			return uuid.Nil, invalid("expires_at must be in the future")
		}
	}
	return id, nil
}

// schemaFor derives the schema name from the parsed id, never from user text.
func schemaFor(id uuid.UUID) string {
	return "t_" + strings.ReplaceAll(id.String(), "-", "")
}

func hashCode(code string) []byte {
	h := sha256.Sum256([]byte(code))
	return h[:]
}

// ForSchema returns a store whose connections see only the given schema. It has its
// own small pool; the caller closes it.
func (s *Store) ForSchema(ctx context.Context, schema string) (*Store, error) {
	cfg := s.cfg.Copy()
	cfg.MaxConns = 4
	cfg.MaxConnIdleTime = 5 * time.Minute
	params := map[string]string{}
	for k, v := range cfg.ConnConfig.RuntimeParams {
		params[k] = v
	}
	params["search_path"] = pgx.Identifier{schema}.Sanitize()
	cfg.ConnConfig.RuntimeParams = params
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool, cfg: cfg, schema: schema}, nil
}

// TenantStore opens the schema and brings it up to date before it serves requests.
func (s *Store) TenantStore(ctx context.Context, schema string) (*Store, error) {
	ts, err := s.ForSchema(ctx, schema)
	if err != nil {
		return nil, err
	}
	sub, err := fs.Sub(migrations, "migrations")
	if err == nil {
		err = ts.migrate(ctx, sub, true)
	}
	if err != nil {
		ts.Close()
		return nil, err
	}
	return ts, nil
}

// MigrateTenants brings every registered tenant schema (suspended ones too) up to date.
// One failing tenant does not stop the others; the errors are joined.
func (s *Store) MigrateTenants(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `select schema_name from tenants order by created_at`)
	if err != nil {
		return err
	}
	schemas, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var errs []error
	for _, schema := range schemas {
		ts, err := s.TenantStore(ctx, schema)
		if err != nil {
			slog.Error("tenant migration failed", "schema", schema, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", schema, err))
			continue
		}
		ts.Close()
	}
	return errors.Join(errs...)
}

const tenantColumns = `tenant_id, name, language, tier, owner_email, schema_name, status, created_at, expires_at`

func scanTenant(row pgx.Row) (Tenant, error) {
	var t Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Language, &t.Tier, &t.OwnerEmail, &t.SchemaName, &t.Status, &t.CreatedAt, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	return t, err
}

func newCode() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// CreateTenant registers a tenant and builds its schema. It is idempotent by id: a
// repeat returns the stored tenant (created=false) and ignores changed fields. Only the
// hash of the access code is stored, so the code is returned only when created=true
// (otherwise it is empty); the caller has to pass it on at once. The
// whole call holds a per-tenant advisory lock, so a concurrent repeat waits until the
// schema is fully migrated.
func (s *Store) CreateTenant(ctx context.Context, in TenantInput) (Tenant, string, bool, error) {
	id, err := in.validate()
	if err != nil {
		return Tenant{}, "", false, err
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return Tenant{}, "", false, err
	}
	defer conn.Release()
	lockKey := "qoimeks-tenant:" + id.String()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock(hashtext($1))`, lockKey); err != nil {
		return Tenant{}, "", false, err
	}
	defer func() {
		// Session lock: if the unlock fails the connection is broken, and closing it frees the lock.
		if _, err := conn.Exec(context.Background(), `select pg_advisory_unlock(hashtext($1))`, lockKey); err != nil {
			conn.Conn().Close(context.Background())
		}
	}()

	tier, err := StoredTier(in.Tier, in.Plan)
	if err != nil {
		return Tenant{}, "", false, err
	}
	code, err := newCode()
	if err != nil {
		return Tenant{}, "", false, err
	}
	schema := schemaFor(id)
	tag, err := conn.Exec(ctx, `
		insert into tenants (tenant_id, name, language, tier, owner_email, schema_name, access_hash, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (tenant_id) do nothing`,
		id, strings.TrimSpace(in.Name), in.Language, tier, in.OwnerEmail, schema, hashCode(code), in.ExpiresAt)
	if err != nil {
		return Tenant{}, "", false, err
	}
	created := tag.RowsAffected() == 1

	// Also on repeats, so a schema that was half built heals.
	if _, err := conn.Exec(ctx, `create schema if not exists `+pgx.Identifier{schema}.Sanitize()); err != nil {
		return Tenant{}, "", false, fmt.Errorf("create schema: %w", err)
	}
	ts, err := s.TenantStore(ctx, schema)
	if err != nil {
		return Tenant{}, "", false, fmt.Errorf("migrate tenant schema: %w", err)
	}
	ts.Close()

	t, err := scanTenant(conn.QueryRow(ctx, `select `+tenantColumns+` from tenants where tenant_id = $1`, id))
	if err != nil {
		return Tenant{}, "", false, err
	}
	if !created {
		code = ""
	}
	return t, code, created, nil
}

func (s *Store) Tenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	return scanTenant(s.pool.QueryRow(ctx, `select `+tenantColumns+` from tenants where tenant_id = $1`, id))
}

// TenantByCode finds the tenant that owns an access code, or ErrNotFound.
func (s *Store) TenantByCode(ctx context.Context, code string) (Tenant, error) {
	return scanTenant(s.pool.QueryRow(ctx, `select `+tenantColumns+` from tenants where access_hash = $1`, hashCode(code)))
}

// SetTenantStatus suspends, resumes or expires a tenant; setting the current status again
// is fine. An expired tenant (stored, or a demo past expires_at as of now) cannot be
// suspended or resumed: ErrTenantExpired. Data in the tenant schema is not touched.
func (s *Store) SetTenantStatus(ctx context.Context, id uuid.UUID, status string, now time.Time) error {
	if status != TenantActive && status != TenantSuspended && status != TenantExpired {
		return invalid("unknown status %q", status)
	}
	tag, err := s.pool.Exec(ctx, `
		update tenants set status = $2, updated_at = now()
		where tenant_id = $1
		  and ($2 = 'expired' or (status <> 'expired' and (expires_at is null or expires_at > $3)))`,
		id, status, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := s.Tenant(ctx, id); err != nil {
			return err
		}
		return ErrTenantExpired
	}
	return nil
}

// RotateTenantCode replaces the tenant's access code and returns the new one, which is
// not stored in clear. The old code stops working at once. Suspended tenants rotate too.
func (s *Store) RotateTenantCode(ctx context.Context, id uuid.UUID) (string, error) {
	code, err := newCode()
	if err != nil {
		return "", err
	}
	tag, err := s.pool.Exec(ctx, `update tenants set access_hash = $2, updated_at = now() where tenant_id = $1`, id, hashCode(code))
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return code, nil
}
