// Package store is the PostgreSQL layer: catalog lookups and the append-only ledger.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed demo_seed.sql
var demoSeed string

const (
	KindReceipt    = "receipt"
	KindIssue      = "issue"
	KindTransfer   = "transfer"
	KindAdjustment = "adjustment"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrInvalid wraps problems with the request itself (unknown SKU, bad kind, ...).
	ErrInvalid = errors.New("invalid")
	// ErrTenantExpired: the tenant is expired, so it cannot be suspended or resumed.
	ErrTenantExpired = errors.New("tenant expired")
	// ErrConflict: the ID was already used for a different operation.
	ErrConflict = errors.New("this operation was already posted with different data")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

type Store struct {
	pool *pgxpool.Pool
	cfg  *pgxpool.Config
}

func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, cfg: cfg}, nil
}

func (s *Store) Close() { s.pool.Close() }

// SeedDemo loads a small demo catalog, but only into an empty database.
func (s *Store) SeedDemo(ctx context.Context) error {
	var n int
	if err := s.pool.QueryRow(ctx, `select count(*) from skus`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, demoSeed)
	return err
}

type SKU struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	Unit    string `json:"unit"`
	Barcode string `json:"barcode,omitempty"`
	// LocationID is where the SKU normally lives; AltLocationID is a second place to look.
	LocationID    *int64 `json:"location_id,omitempty"`
	AltLocationID *int64 `json:"alt_location_id,omitempty"`
}

type Location struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type Catalog struct {
	SKUs      []SKU      `json:"skus"`
	Locations []Location `json:"locations"`
}

// Catalog returns the active SKUs and locations, each sorted by code.
func (s *Store) Catalog(ctx context.Context) (Catalog, error) {
	c := Catalog{SKUs: []SKU{}, Locations: []Location{}}
	rows, err := s.pool.Query(ctx, `select id, code, name, unit, coalesce(barcode, ''), location_id, alt_location_id from skus where active order by code`)
	if err != nil {
		return c, err
	}
	c.SKUs, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SKU, error) {
		var v SKU
		return v, r.Scan(&v.ID, &v.Code, &v.Name, &v.Unit, &v.Barcode, &v.LocationID, &v.AltLocationID)
	})
	if err != nil {
		return c, err
	}
	rows, err = s.pool.Query(ctx, `select id, code, name from locations where active order by code`)
	if err != nil {
		return c, err
	}
	c.Locations, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Location, error) {
		var v Location
		return v, r.Scan(&v.ID, &v.Code, &v.Name)
	})
	return c, err
}

type Balance struct {
	SKUID        int64   `json:"sku_id"`
	LocationID   int64   `json:"location_id"`
	LocationCode string  `json:"location_code"`
	Delta        float64 `json:"delta,omitempty"`
	Qty          float64 `json:"qty"`
}

// Balances returns the current non-zero balances of one SKU, by location.
func (s *Store) Balances(ctx context.Context, skuID int64) ([]Balance, error) {
	rows, err := s.pool.Query(ctx, `
		select sku_id, location_id, code, qty from (
			select distinct on (b.location_id) b.sku_id, b.location_id, l.code, b.qty::float8 as qty
			from balances b join locations l on l.id = b.location_id
			where b.sku_id = $1
			order by b.location_id, b.id desc
		) cur
		where qty <> 0
		order by code`, skuID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Balance, error) {
		var v Balance
		return v, r.Scan(&v.SKUID, &v.LocationID, &v.LocationCode, &v.Qty)
	})
}

// NewMovement is what an operator submits.
type NewMovement struct {
	ID           uuid.UUID `json:"id"`
	Kind         string    `json:"kind"`
	SKUID        int64     `json:"sku_id"`
	LocationID   int64     `json:"location_id"`
	ToLocationID *int64    `json:"to_location_id"`
	Qty          float64   `json:"qty"`
	Operator     string    `json:"operator"`
	Source       string    `json:"source"`
	Transcript   string    `json:"transcript"`
}

type Movement struct {
	ID             uuid.UUID  `json:"id"`
	Seq            int64      `json:"seq"`
	Kind           string     `json:"kind"`
	SKUID          int64      `json:"sku_id"`
	SKUCode        string     `json:"sku_code"`
	SKUName        string     `json:"sku_name"`
	Unit           string     `json:"unit"`
	LocationID     int64      `json:"location_id"`
	LocationCode   string     `json:"location_code"`
	ToLocationID   *int64     `json:"to_location_id,omitempty"`
	ToLocationCode string     `json:"to_location_code,omitempty"`
	Qty            float64    `json:"qty"`
	Operator       string     `json:"operator"`
	Source         string     `json:"source"`
	Transcript     string     `json:"transcript,omitempty"`
	Reverses       *uuid.UUID `json:"reverses,omitempty"`
	ReversedBy     *uuid.UUID `json:"reversed_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	Balances       []Balance  `json:"balances"`
}

const movementSelect = `
	select m.id, m.seq, m.kind, m.sku_id, s.code, s.name, s.unit,
	       m.location_id, l.code, m.to_location_id, coalesce(tl.code, ''),
	       m.qty::float8, m.operator, m.source, coalesce(m.transcript, ''),
	       m.reverses, r.id, m.created_at
	from movements m
	join skus s on s.id = m.sku_id
	join locations l on l.id = m.location_id
	left join locations tl on tl.id = m.to_location_id
	left join movements r on r.reverses = m.id`

func scanMovement(r pgx.CollectableRow) (Movement, error) {
	var m Movement
	err := r.Scan(&m.ID, &m.Seq, &m.Kind, &m.SKUID, &m.SKUCode, &m.SKUName, &m.Unit,
		&m.LocationID, &m.LocationCode, &m.ToLocationID, &m.ToLocationCode,
		&m.Qty, &m.Operator, &m.Source, &m.Transcript,
		&m.Reverses, &m.ReversedBy, &m.CreatedAt)
	m.Balances = []Balance{}
	return m, err
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func getMovement(ctx context.Context, q querier, id uuid.UUID) (Movement, error) {
	rows, err := q.Query(ctx, movementSelect+` where m.id = $1`, id)
	if err != nil {
		return Movement{}, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, scanMovement)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	rows, err = q.Query(ctx, `
		select b.sku_id, b.location_id, l.code, b.delta::float8, b.qty::float8
		from balances b join locations l on l.id = b.location_id
		where b.movement_id = $1 order by b.id`, id)
	if err != nil {
		return m, err
	}
	m.Balances, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Balance, error) {
		var v Balance
		return v, r.Scan(&v.SKUID, &v.LocationID, &v.LocationCode, &v.Delta, &v.Qty)
	})
	return m, err
}

// Movements returns the most recent movements, newest first (without balance rows).
func (s *Store) Movements(ctx context.Context, limit int) ([]Movement, error) {
	rows, err := s.pool.Query(ctx, movementSelect+` order by m.seq desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanMovement)
}

// appendBalance locks one (sku, location) pair for the rest of the transaction and
// appends a balance row. Exactly one of delta / setTo is used: delta shifts the
// balance, setTo replaces it (stock count).
func appendBalance(ctx context.Context, tx pgx.Tx, movementID uuid.UUID, skuID, locationID int64, delta float64, setTo *float64) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1::int, $2::int)`, skuID, locationID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		with cur as (
			select coalesce((select qty from balances where sku_id = $2 and location_id = $3 order by id desc limit 1), 0) as qty
		)
		insert into balances (movement_id, sku_id, location_id, delta, qty)
		select $1, $2, $3,
		       coalesce($5::numeric - cur.qty, $4::numeric),
		       coalesce($5::numeric, cur.qty + $4::numeric)
		from cur`, movementID, skuID, locationID, delta, setTo)
	return err
}

func asInvalid(err error) error {
	var pgErr *pgconn.PgError
	// 23xxx: integrity constraint violations (unknown SKU/location, failed checks).
	if errors.As(err, &pgErr) && len(pgErr.Code) == 5 && pgErr.Code[:2] == "23" {
		return invalid("%s", pgErr.Message)
	}
	return err
}

// Post records a movement and the balance rows it causes. Posting the same ID
// twice is safe: the second call changes nothing and returns the stored movement.
func (s *Store) Post(ctx context.Context, in NewMovement) (Movement, bool, error) {
	switch in.Kind {
	case KindReceipt, KindIssue, KindTransfer, KindAdjustment:
	default:
		return Movement{}, false, invalid("unknown kind %q", in.Kind)
	}
	if in.ID == uuid.Nil {
		return Movement{}, false, invalid("id is required")
	}
	if in.Operator == "" {
		return Movement{}, false, invalid("operator is required")
	}
	if in.Qty < 0 || (in.Qty == 0 && in.Kind != KindAdjustment) {
		return Movement{}, false, invalid("quantity must be positive")
	}
	if (in.Kind == KindTransfer) != (in.ToLocationID != nil) {
		return Movement{}, false, invalid("to_location_id is required for a transfer and only for a transfer")
	}
	if in.ToLocationID != nil && *in.ToLocationID == in.LocationID {
		return Movement{}, false, invalid("transfer needs two different locations")
	}

	var out Movement
	created := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			insert into movements (id, kind, sku_id, location_id, to_location_id, qty, operator, source, transcript)
			values ($1, $2, $3, $4, $5, $6::numeric, $7, $8, nullif($9, ''))
			on conflict (id) do nothing`,
			in.ID, in.Kind, in.SKUID, in.LocationID, in.ToLocationID, in.Qty, in.Operator, in.Source, in.Transcript)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			created = true
			switch in.Kind {
			case KindReceipt:
				err = appendBalance(ctx, tx, in.ID, in.SKUID, in.LocationID, in.Qty, nil)
			case KindIssue:
				err = appendBalance(ctx, tx, in.ID, in.SKUID, in.LocationID, -in.Qty, nil)
			case KindAdjustment:
				err = appendBalance(ctx, tx, in.ID, in.SKUID, in.LocationID, 0, &in.Qty)
			case KindTransfer:
				// Lock in a fixed order so two opposite transfers cannot deadlock.
				legs := []struct {
					loc   int64
					delta float64
				}{{in.LocationID, -in.Qty}, {*in.ToLocationID, in.Qty}}
				sort.Slice(legs, func(i, j int) bool { return legs[i].loc < legs[j].loc })
				for _, leg := range legs {
					if err = appendBalance(ctx, tx, in.ID, in.SKUID, leg.loc, leg.delta, nil); err != nil {
						break
					}
				}
			}
			if err != nil {
				return err
			}
		}
		out, err = getMovement(ctx, tx, in.ID)
		if err != nil {
			return err
		}
		sameTo := (in.ToLocationID == nil && out.ToLocationID == nil) ||
			(in.ToLocationID != nil && out.ToLocationID != nil && *in.ToLocationID == *out.ToLocationID)
		if !created && (out.Reverses != nil || out.Kind != in.Kind || out.SKUID != in.SKUID ||
			out.LocationID != in.LocationID || !sameTo || out.Qty != in.Qty) {
			return ErrConflict
		}
		return nil
	})
	return out, created, asInvalid(err)
}

// Reverse cancels a movement by appending a new one that undoes its balance changes.
func (s *Store) Reverse(ctx context.Context, originalID, newID uuid.UUID, operator string) (Movement, bool, error) {
	if newID == uuid.Nil {
		return Movement{}, false, invalid("id is required")
	}
	if operator == "" {
		return Movement{}, false, invalid("operator is required")
	}
	var out Movement
	created := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		out, created, err = reverse(ctx, tx, originalID, newID, operator, "keyboard")
		return err
	})
	return out, created, asInvalid(err)
}

func reverse(ctx context.Context, tx pgx.Tx, originalID, newID uuid.UUID, operator, source string) (Movement, bool, error) {
	orig, err := getMovement(ctx, tx, originalID)
	if err != nil {
		return Movement{}, false, err
	}
	if orig.Reverses != nil {
		return Movement{}, false, invalid("a reversal cannot be reversed; post a new operation instead")
	}
	if orig.ReversedBy != nil && *orig.ReversedBy != newID {
		return Movement{}, false, invalid("already reversed")
	}
	tag, err := tx.Exec(ctx, `
		insert into movements (id, kind, sku_id, location_id, to_location_id, qty, operator, source, reverses)
		values ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9)
		on conflict (id) do nothing`,
		newID, orig.Kind, orig.SKUID, orig.LocationID, orig.ToLocationID, orig.Qty, operator, source, originalID)
	if err != nil {
		return Movement{}, false, err
	}
	created := tag.RowsAffected() == 1
	if created {
		legs := orig.Balances
		sort.Slice(legs, func(i, j int) bool { return legs[i].LocationID < legs[j].LocationID })
		for _, leg := range legs {
			if err := appendBalance(ctx, tx, newID, leg.SKUID, leg.LocationID, -leg.Delta, nil); err != nil {
				return Movement{}, false, err
			}
		}
	}
	out, err := getMovement(ctx, tx, newID)
	return out, created, err
}
