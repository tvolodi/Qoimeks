package importer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"qoimeks/server/internal/store"
)

func TestParse(t *testing.T) {
	f := excelize.NewFile()
	rows := [][]any{
		{},
		{"Склад"},
		{"Артикул", "Номенклатура", "Конечный остаток"},
		{"Основной"},
		{"A-1 ", "A-1  Замок", 10, "R", "I", 2, "RK2"},
		{"A-2", "Кнопка", nil, "C"},
		{"A-3", "A-3 Поручни", 0, "L", "M", 3},
		{"A-3", "A-3 ПОРУЧНИ СТАЛЬНЫЕ", 6, "L", "G", 1}, // same article, second place
		{"A-4", "A-4 Датчик", 5, "L", 0, 1},             // zero typed for the letter O
		{"A-5", "A-5 Доводчик", 7, "С"},                 // Cyrillic С
		{"A-6", "A-6 Адаптер", 8, nil, "B", 2},          // stock without a zone
		{"A-7", "A-7 Пружина"},                          // no place, no stock
		{"A-8", "B-9 Чужой товар", 30, "R", "O", 2},     // wrong article on this row...
		{"A-8", "A-8 Кнопка выхода", 26, "L", "N", 2},   // ...the real A-8
		{"A-9", "Карта INDALA", 1, "D", "A", 1},
		{"A-9", "Карта Prox", 2, "D", "A", 2, 0},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "stock.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}

	res, err := Parse(path, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]store.ImportSKU{}
	for _, s := range res.SKUs {
		got[s.Code] = s
	}
	check := func(code, name, home, alt string, stock ...store.ImportStock) {
		t.Helper()
		s, ok := got[code]
		if !ok {
			t.Fatalf("%s: not imported", code)
		}
		if s.Name != name || s.Home != home || s.Alt != alt || len(s.Stock) != len(stock) {
			t.Fatalf("%s: got %+v", code, s)
		}
		for i := range stock {
			if s.Stock[i] != stock[i] {
				t.Fatalf("%s: stock %+v, want %+v", code, s.Stock, stock)
			}
		}
	}
	check("A-1", "A-1 Замок", "RI2", "RK2", store.ImportStock{Location: "RI2", Qty: 10})
	check("A-2", "Кнопка", "C", "", store.ImportStock{Location: "C", Qty: 0})
	check("A-3", "A-3 Поручни", "LM3", "LG1", store.ImportStock{Location: "LM3", Qty: 0}, store.ImportStock{Location: "LG1", Qty: 6})
	check("A-4", "A-4 Датчик", "LO1", "", store.ImportStock{Location: "LO1", Qty: 5})
	check("A-5", "A-5 Доводчик", "C", "", store.ImportStock{Location: "C", Qty: 7})
	check("A-6", "A-6 Адаптер", NoLocation, "", store.ImportStock{Location: NoLocation, Qty: 8})
	check("A-7", "A-7 Пружина", "", "")
	check("A-8", "A-8 Кнопка выхода", "LN2", "", store.ImportStock{Location: "LN2", Qty: 26})
	check("A-9", "Карта INDALA", "DA1", "", store.ImportStock{Location: "DA1", Qty: 1})
	if len(res.SKUs) != 9 {
		t.Fatalf("%d SKUs", len(res.SKUs))
	}

	names := map[string]string{}
	for _, l := range res.Locations {
		names[l.Code] = l.Name
	}
	if names["RI2"] != "Справа I 2" || names["C"] != "" || len(names) != 11 {
		t.Fatalf("locations: %v", names)
	}

	all := strings.Join(res.Warnings, "\n")
	for _, want := range []string{
		`row 9: A-4: place "L" "0" 1 read as LO1`,
		`row 10: A-5: place "С"`,
		`row 11: A-6: balance 8 but no place`,
		`row 14: A-8: NOT IMPORTED: "B-9 Чужой товар" (balance 30)`,
		`row 16: A-9: NOT IMPORTED: "Карта Prox" (balance 2)`,
		`row 16: A-9: second place "0"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing remark %q in:\n%s", want, all)
		}
	}
}
