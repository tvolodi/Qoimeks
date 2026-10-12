package importer

import (
	"bytes"
	"reflect"
	"testing"

	"qoimeks/server/internal/store"
)

// What Write saves, Parse reads back: places, usual place and second place included.
func TestWriteThenParse(t *testing.T) {
	items := []store.StockItem{
		{Code: "Z-1", Name: "Z-1 Замок", Unit: "шт", Home: "RI2", Alt: "RK2", Total: 13, Places: []store.StockPlace{
			{LocationCode: "LA1", Qty: 3}, {LocationCode: "RI2", Qty: 10}}},
		{Code: "Z-2", Name: "Z-2 Кнопка", Unit: "шт", Home: "LA1", Places: []store.StockPlace{{LocationCode: "RK2", Qty: 4.5}}},
		{Code: "Z-3", Name: "Z-3 Пусто", Unit: "шт"},
	}
	var buf bytes.Buffer
	if err := Write(&buf, "Остатки 2026-10-10", items); err != nil {
		t.Fatal(err)
	}
	got, err := ParseReader(&buf, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings: %v", got.Warnings)
	}
	// Z-2 keeps its stock in a second place, which Parse takes as the place to look next.
	want := []store.ImportSKU{
		{Code: "Z-1", Name: "Z-1 Замок", Home: "RI2", Alt: "RK2", Stock: []store.ImportStock{{Location: "RI2", Qty: 10, Row: 2}, {Location: "LA1", Qty: 3, Row: 3}}},
		{Code: "Z-2", Name: "Z-2 Кнопка", Home: "LA1", Alt: "RK2", Stock: []store.ImportStock{{Location: "LA1", Qty: 0, Row: 4}, {Location: "RK2", Qty: 4.5, Row: 5}}},
		{Code: "Z-3", Name: "Z-3 Пусто"},
	}
	if !reflect.DeepEqual(got.SKUs, want) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got.SKUs, want)
	}
}
