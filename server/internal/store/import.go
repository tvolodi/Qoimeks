package store

import (
	"context"

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
}

type ImportResult struct {
	LocationsCreated int
	SKUsCreated      int
	SKUsUpdated      int
	BalancesPosted   int
}

// Import upserts the catalog in one transaction. With postBalances it also records
// the file's quantities as stock counts, but only for SKU/location pairs the ledger
// knows nothing about yet: balances that operators already work with are never overwritten.
func (s *Store) Import(ctx context.Context, locations []ImportLocation, skus []ImportSKU, postBalances bool, operator string) (ImportResult, error) {
	var res ImportResult
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
			if !postBalances {
				continue
			}
			for _, st := range sku.Stock {
				loc := ref(st.Location)
				if loc == nil || st.Qty <= 0 {
					continue
				}
				var known bool
				if err := tx.QueryRow(ctx, `select exists(select 1 from balances where sku_id = $1 and location_id = $2)`, skuID, *loc).Scan(&known); err != nil {
					return err
				}
				if known {
					continue
				}
				movementID := uuid.New()
				if _, err := tx.Exec(ctx, `
					insert into movements (id, kind, sku_id, location_id, qty, operator, source)
					values ($1, 'adjustment', $2, $3, $4::numeric, $5, 'import')`,
					movementID, skuID, *loc, st.Qty, operator); err != nil {
					return err
				}
				qty := st.Qty
				if err := appendBalance(ctx, tx, movementID, skuID, *loc, 0, &qty); err != nil {
					return err
				}
				res.BalancesPosted++
			}
		}
		return nil
	})
	return res, err
}
