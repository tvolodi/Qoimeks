package store

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ImportLocation struct {
	Code string
	Name string
}

type ImportStock struct {
	Location string
	Qty      float64
}

// ImportSKU is one article from the Excel file. Home and Alt are location codes
// or empty; Stock lists the quantity the file states at each location.
type ImportSKU struct {
	Code  string
	Name  string
	Home  string
	Alt   string
	Stock []ImportStock
	// KeepBalances: a balance of this article could not be read, so none of its
	// balances are touched.
	KeepBalances bool
}

type ImportResult struct {
	LocationsCreated int `json:"locations_created"`
	SKUsCreated      int `json:"skus_created"`
	SKUsUpdated      int `json:"skus_updated"`
	BalancesPosted   int `json:"balances_posted"`
}

// ImportRecord is the audit entry of one import: who loaded which workbook and
// what it changed. The workbook itself is not kept.
// BalanceChange is one balance the import replaces with the file's quantity.
type BalanceChange struct {
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Location string  `json:"location"`
	Was      float64 `json:"was"`
	Now      float64 `json:"now"`
}

// ImportReport is ImportResult with the details a person checks before applying.
type ImportReport struct {
	ImportResult
	// Changes lists the balances that differ from the file, in file order.
	Changes []BalanceChange `json:"balance_changes"`
	// NotInFile lists stock of SKUs the file does not mention; it is left as it is.
	NotInFile []BalanceChange `json:"not_in_file"`
}

type ImportRecord struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	Who          string    `json:"who"`
	Filename     string    `json:"filename"`
	Sheet        string    `json:"sheet"`
	SHA256       string    `json:"sha256"`
	WithBalances bool      `json:"with_balances"`
	Remarks      int       `json:"remarks"`
	// Cancelled: the balance changes of this import were undone later.
	Cancelled bool `json:"cancelled"`
	ImportResult
}

type ImportOptions struct {
	// PostBalances also makes the ledger agree with the file: wherever a balance
	// differs from the file's quantity, a stock-count movement sets it to the file's.
	// A SKU of the file is emptied at places the file does not list for it; SKUs the
	// file does not mention are left alone.
	PostBalances bool
	// DryRun computes the result and then undoes everything.
	DryRun bool
	// Operator is the name written on those movements.
	Operator string
	// Record, when set, is saved as the audit entry of a real import.
	Record *ImportRecord
}

var errDryRun = errors.New("dry run")

// Import upserts the catalog in one transaction.
func (s *Store) Import(ctx context.Context, locations []ImportLocation, skus []ImportSKU, opts ImportOptions) (ImportReport, error) {
	res := ImportReport{Changes: []BalanceChange{}, NotInFile: []BalanceChange{}}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// The import's number is taken first, so its movements can carry it.
		var importID *int64
		if opts.Record != nil && !opts.DryRun {
			if err := tx.QueryRow(ctx, `select nextval(pg_get_serial_sequence('imports', 'id'))`).Scan(&importID); err != nil {
				return err
			}
		}
		for _, l := range locations {
			tag, err := tx.Exec(ctx, `insert into locations (code, name) values ($1, $2) on conflict (code) do nothing`, l.Code, l.Name)
			if err != nil {
				return err
			}
			res.LocationsCreated += int(tag.RowsAffected())
		}
		locID := map[string]int64{}
		rows, err := tx.Query(ctx, `select code, id from locations`)
		if err != nil {
			return err
		}
		var code string
		var id int64
		if _, err := pgx.ForEachRow(rows, []any{&code, &id}, func() error { locID[code] = id; return nil }); err != nil {
			return err
		}
		ref := func(code string) *int64 {
			if v, ok := locID[code]; ok {
				return &v
			}
			return nil
		}

		for _, sku := range skus {
			var skuID int64
			var created bool
			err := tx.QueryRow(ctx, `
				insert into skus (code, name, location_id, alt_location_id) values ($1, $2, $3, $4)
				on conflict (code) do update
					set name = excluded.name, location_id = excluded.location_id,
					    alt_location_id = excluded.alt_location_id, active = true
				returning id, xmax = 0`,
				sku.Code, sku.Name, ref(sku.Home), ref(sku.Alt)).Scan(&skuID, &created)
			if err != nil {
				return err
			}
			if created {
				res.SKUsCreated++
			} else {
				res.SKUsUpdated++
			}
			if !opts.PostBalances || sku.KeepBalances {
				continue
			}
			changes, err := importBalances(ctx, tx, skuID, sku, ref, opts.Operator, importID)
			if err != nil {
				return err
			}
			res.Changes = append(res.Changes, changes...)
			res.BalancesPosted += len(changes)
		}
		if opts.PostBalances {
			codes := make([]string, len(skus))
			for i, sku := range skus {
				codes[i] = sku.Code
			}
			rows, err := tx.Query(ctx, currentBalances+`
				select k.code, k.name, l.code, c.qty::float8
				from cur c join skus k on k.id = c.sku_id join locations l on l.id = c.location_id
				where c.qty <> 0 and k.active and k.code <> all($1)
				order by k.code, l.code`, codes)
			if err != nil {
				return err
			}
			res.NotInFile, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (BalanceChange, error) {
				var v BalanceChange
				err := r.Scan(&v.Code, &v.Name, &v.Location, &v.Was)
				v.Now = v.Was
				return v, err
			})
			if err != nil {
				return err
			}
		}
		if opts.DryRun {
			return errDryRun
		}
		if r := opts.Record; r != nil {
			_, err := tx.Exec(ctx, `
				insert into imports (id, who, filename, sheet, sha256, with_balances, remarks,
				                     skus_created, skus_updated, locations_created, balances_posted)
				overriding system value
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				importID, r.Who, r.Filename, r.Sheet, r.SHA256, r.WithBalances, r.Remarks,
				res.SKUsCreated, res.SKUsUpdated, res.LocationsCreated, res.BalancesPosted)
			return err
		}
		return nil
	})
	if errors.Is(err, errDryRun) {
		err = nil
	}
	return res, err
}

const currentBalances = `
	with cur as (
		select distinct on (sku_id, location_id) sku_id, location_id, qty
		from balances
		order by sku_id, location_id, id desc
	)`

// importBalances makes the balances of one SKU agree with the file and returns
// what it changed. Each difference becomes a stock-count movement, so the history
// shows where the file disagreed with the ledger.
func importBalances(ctx context.Context, tx pgx.Tx, skuID int64, sku ImportSKU, ref func(string) *int64, operator string, importID *int64) ([]BalanceChange, error) {
	type place struct {
		code   string
		was    float64
		target float64
	}
	places := map[int64]*place{}
	var order []int64
	rows, err := tx.Query(ctx, currentBalances+`
		select c.location_id, l.code, c.qty::float8
		from cur c join locations l on l.id = c.location_id
		where c.sku_id = $1 and c.qty <> 0`, skuID)
	if err != nil {
		return nil, err
	}
	var id int64
	var code string
	var qty float64
	if _, err := pgx.ForEachRow(rows, []any{&id, &code, &qty}, func() error {
		places[id] = &place{code: code, was: qty}
		order = append(order, id)
		return nil
	}); err != nil {
		return nil, err
	}
	for _, st := range sku.Stock {
		loc := ref(st.Location)
		if loc == nil {
			continue
		}
		if places[*loc] == nil {
			places[*loc] = &place{code: st.Location}
			order = append(order, *loc)
		}
		places[*loc].target += st.Qty
	}
	// Lock in a fixed order, as transfers do.
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	var changes []BalanceChange
	for _, loc := range order {
		p := places[loc]
		if math.Abs(p.target-p.was) < 0.0005 { // balances keep three decimals
			continue
		}
		movementID := uuid.New()
		if _, err := tx.Exec(ctx, `
			insert into movements (id, kind, sku_id, location_id, qty, operator, source, import_id)
			values ($1, 'adjustment', $2, $3, $4::numeric, $5, 'import', $6)`,
			movementID, skuID, loc, p.target, operator, importID); err != nil {
			return nil, err
		}
		if err := appendBalance(ctx, tx, movementID, skuID, loc, 0, &p.target); err != nil {
			return nil, err
		}
		changes = append(changes, BalanceChange{Code: sku.Code, Name: sku.Name, Location: p.code, Was: p.was, Now: p.target})
	}
	return changes, nil
}

// Imports returns the most recent audit entries, newest first.
func (s *Store) Imports(ctx context.Context, limit int) ([]ImportRecord, error) {
	rows, err := s.pool.Query(ctx, `
		select i.id, i.created_at, i.who, i.filename, i.sheet, i.sha256, i.with_balances, i.remarks,
		       i.skus_created, i.skus_updated, i.locations_created, i.balances_posted,
		       exists(select 1 from movements m join movements r on r.reverses = m.id where m.import_id = i.id)
		from imports i order by i.id desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ImportRecord, error) {
		var v ImportRecord
		return v, r.Scan(&v.ID, &v.CreatedAt, &v.Who, &v.Filename, &v.Sheet, &v.SHA256, &v.WithBalances, &v.Remarks,
			&v.SKUsCreated, &v.SKUsUpdated, &v.LocationsCreated, &v.BalancesPosted, &v.Cancelled)
	})
}

// CancelImport undoes the balance changes of one import and returns how many it
// undid. Nothing is deleted: every movement of the import gets a reversal, which
// takes back exactly the difference the file made, so what operators posted after
// the import stays. Names and places the import wrote into the catalog also stay.
func (s *Store) CancelImport(ctx context.Context, importID int64, operator string) (int, error) {
	if operator == "" {
		return 0, invalid("operator is required")
	}
	n := 0
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var found bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from imports where id = $1)`, importID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, `
			select m.id from movements m
			where m.import_id = $1 and not exists(select 1 from movements r where r.reverses = m.id)
			order by m.seq desc`, importID)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return invalid("this import has no balance changes left to cancel")
		}
		for _, id := range ids {
			if _, _, err := reverse(ctx, tx, id, uuid.New(), operator, "import"); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, asInvalid(err)
}
