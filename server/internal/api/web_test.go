package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/importer"
	"qoimeks/server/internal/store"
)

func TestStockOverviewAndExport(t *testing.T) {
	c, skus, locs := setup(t, &Server{AccessToken: "phone"})
	for loc, qty := range map[string]float64{"A-01": 100, "B-01": 5} {
		c.do("POST", "/api/movements", store.NewMovement{
			ID: uuid.New(), Kind: "receipt", SKUID: skus["NUT-M8"], LocationID: locs[loc], Qty: qty, Operator: "Айгуль",
		}, 201, nil)
	}

	var items []store.StockItem
	c.do("GET", "/api/stock", nil, 200, &items)
	var nut *store.StockItem
	for i := range items {
		if items[i].Code == "NUT-M8" {
			nut = &items[i]
		}
	}
	if nut == nil || nut.Total != 105 || len(nut.Places) != 2 || nut.Places[0].LocationCode != "A-01" || nut.Places[1].Qty != 5 {
		t.Fatalf("NUT-M8: %+v", nut)
	}
	if len(items) != len(skus) {
		t.Fatalf("%d items for %d SKUs: SKUs without stock must be listed too", len(items), len(skus))
	}

	// The page is open; the data and the file need the access code.
	if resp, err := http.Get(c.url + "/"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("page: %v %v", resp, err)
	}
	resp, err := http.Get(c.url + "/api/stock/export")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("export without a code: %v %v", resp, err)
	}
	req, _ := http.NewRequest("GET", c.url+"/api/stock/export", nil)
	req.Header.Set("Authorization", "Bearer phone")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != xlsxType ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", resp.StatusCode, resp.Header)
	}
	parsed, err := importer.ParseReader(resp.Body, "")
	if err != nil {
		t.Fatalf("the export is not readable by the importer: %v", err)
	}
	if len(parsed.SKUs) != len(skus) {
		t.Fatalf("export has %d SKUs, want %d", len(parsed.SKUs), len(skus))
	}
}
