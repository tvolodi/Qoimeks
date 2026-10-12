package importer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"qoimeks/server/internal/store"
)

func TestQuantity(t *testing.T) {
	for raw, want := range map[string]float64{
		"10": 10, "1.5": 1.5, "1,5": 1.5, "1,000": 1, "0,250": 0.25, "1E+3": 1000,
		"1 250": 1250, "1 250,5": 1250.5, "1'250.5": 1250.5,
		"1.250,75": 1250.75, "1,250.75": 1250.75, "1.234.567": 1234567, "1,234,567": 1234567,
		"1.234.567,8": 1234567.8,
	} {
		if got, ok := quantity(raw); !ok || got != want {
			t.Errorf("%q: got %v %v, want %v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"-3", "много", "10 шт", "1,2,3.4.5", "1.2,3,4", "NaN", ""} {
		if got, ok := quantity(raw); ok {
			t.Errorf("%q: read as %v", raw, got)
		}
	}
}

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
		{"A-1", "A-1 Замок", 5, "R", "I", 2}, // the same article and place again
		{"A-10", "A-10 Петля", "1 250,5", "L", "A", 1},
		{"A-11", "A-11 Ручка", "много", "L", "A", 2},
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
	check("A-1", "A-1 Замок", "RI2", "RK2", store.ImportStock{Location: "RI2", Qty: 15})
	check("A-2", "Кнопка", "C", "", store.ImportStock{Location: "C", Qty: 0})
	check("A-3", "A-3 Поручни", "LM3", "LG1", store.ImportStock{Location: "LM3", Qty: 0}, store.ImportStock{Location: "LG1", Qty: 6})
	check("A-4", "A-4 Датчик", "LO1", "", store.ImportStock{Location: "LO1", Qty: 5})
	check("A-5", "A-5 Доводчик", "C", "", store.ImportStock{Location: "C", Qty: 7})
	check("A-6", "A-6 Адаптер", NoLocation, "", store.ImportStock{Location: NoLocation, Qty: 8})
	check("A-7", "A-7 Пружина", "", "")
	check("A-8", "A-8 Кнопка выхода", "LN2", "RO2", store.ImportStock{Location: "RO2", Qty: 30}, store.ImportStock{Location: "LN2", Qty: 26})
	check("A-9", "Карта INDALA", "DA1", "DA2", store.ImportStock{Location: "DA1", Qty: 1}, store.ImportStock{Location: "DA2", Qty: 2})
	check("A-10", "A-10 Петля", "LA1", "", store.ImportStock{Location: "LA1", Qty: 1250.5})
	check("A-11", "A-11 Ручка", "LA2", "")
	if !got["A-11"].KeepBalances || got["A-10"].KeepBalances {
		t.Fatalf("only an unreadable balance keeps the ledger's: %+v %+v", got["A-10"], got["A-11"])
	}
	if len(res.SKUs) != 11 {
		t.Fatalf("%d SKUs", len(res.SKUs))
	}

	names := map[string]string{}
	for _, l := range res.Locations {
		names[l.Code] = l.Name
	}
	if names["RI2"] != "Справа I 2" || names["C"] != "" || len(names) != 13 {
		t.Fatalf("locations: %v", names)
	}

	all := strings.Join(res.Warnings, "\n")
	if strings.Contains(all, "A-3") {
		t.Errorf("an article kept in two places is not worth a remark:\n%s", all)
	}
	for _, want := range []string{
		`строка 9: A-4: место "L" "0" 1 прочитано как LO1`,
		`строка 10: A-5: место "С"`,
		`строка 11: A-6: остаток 8 без места`,
		`строка 14: A-8: артикул повторяется с другим названием "B-9 Чужой товар". Остатки сложены, товар назван "A-8 Кнопка выхода"`,
		`строка 16: A-9: артикул повторяется с другим названием "Карта Prox". Остатки сложены, товар назван "Карта INDALA"`,
		`строка 16: A-9: второе место "0"`,
		`строка 17: A-1: место RI2 указано повторно, остатки сложены: 15`,
		`строка 19: A-11: остаток "много" не является количеством. Остатки этого товара в системе не изменяются`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing remark %q in:\n%s", want, all)
		}
	}
}
