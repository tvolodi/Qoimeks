package store

import "context"

// StockPlace is the current balance of one SKU at one location.
type StockPlace struct {
	LocationCode string  `json:"location_code"`
	LocationName string  `json:"location_name"`
	Qty          float64 `json:"qty"`
}

// StockItem is one SKU with everything on hand, by location. Places lists only
// non-zero balances, so a SKU with nothing in stock has none.
type StockItem struct {
	SKUID  int64        `json:"sku_id"`
	Code   string       `json:"code"`
	Name   string       `json:"name"`
	Unit   string       `json:"unit"`
	Home   string       `json:"home,omitempty"` // code of the usual location
	Alt    string       `json:"alt,omitempty"`  // code of the second place to look
	Total  float64      `json:"total"`
	Places []StockPlace `json:"places"`
}

// Stock returns every active SKU, sorted by code, with its current non-zero
// balances by location (sorted by location code).
func (s *Store) Stock(ctx context.Context) ([]StockItem, error) {
	rows, err := s.pool.Query(ctx, `
		with cur as (
			select distinct on (sku_id, location_id) sku_id, location_id, qty
			from balances
			order by sku_id, location_id, id desc
		)
		select k.id, k.code, k.name, k.unit, coalesce(h.code, ''), coalesce(a.code, ''),
		       coalesce(l.code, ''), coalesce(l.name, ''), coalesce(c.qty, 0)::float8
		from skus k
		left join locations h on h.id = k.location_id
		left join locations a on a.id = k.alt_location_id
		left join cur c on c.sku_id = k.id and c.qty <> 0
		left join locations l on l.id = c.location_id
		where k.active
		order by k.code, l.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []StockItem{}
	for rows.Next() {
		var id int64
		var code, name, unit, home, alt string
		var p StockPlace
		if err := rows.Scan(&id, &code, &name, &unit, &home, &alt, &p.LocationCode, &p.LocationName, &p.Qty); err != nil {
			return nil, err
		}
		if n := len(items); n == 0 || items[n-1].SKUID != id {
			items = append(items, StockItem{SKUID: id, Code: code, Name: name, Unit: unit, Home: home, Alt: alt, Places: []StockPlace{}})
		}
		if p.LocationCode != "" {
			it := &items[len(items)-1]
			it.Places = append(it.Places, p)
			it.Total += p.Qty
		}
	}
	return items, rows.Err()
}
