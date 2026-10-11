package importer

import (
	"io"
	"sort"
	"strconv"

	"github.com/xuri/excelize/v2"

	"qoimeks/server/internal/store"
)

// Write saves the current stock as a workbook in the layout Parse reads, so the
// file can be corrected in Excel and loaded back. A SKU takes one row per place
// that holds it, its usual place first (with a zero balance if empty there, so
// loading the file back keeps that place). Columns H and I are for people and
// are ignored on loading.
func Write(w io.Writer, sheet string, items []store.StockItem) error {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	header := []any{"Артикул", "Номенклатура", "Конечный остаток", "Зона", "Стеллаж", "Полка", "Второе место", "Место", "Ед. изм."}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		return err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", "I1", bold); err != nil {
		return err
	}
	for col, width := range []float64{18, 44, 18, 8, 10, 8, 14, 12, 10} {
		name, _ := excelize.ColumnNumberToName(col + 1)
		if err := f.SetColWidth(sheet, name, name, width); err != nil {
			return err
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}

	row := 2
	put := func(it store.StockItem, place string, qty float64) error {
		var zone, rack, shelf string
		if m := altShape.FindStringSubmatch(place); m != nil {
			zone, rack, shelf = m[1], m[2], m[3]
		}
		var shelfCell any = shelf
		if shelf != "" {
			if n, err := strconv.Atoi(shelf); err == nil {
				shelfCell = n
			}
		}
		cells := []any{it.Code, it.Name, qty, zone, rack, shelfCell, it.Alt, place, it.Unit}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		row++
		return f.SetSheetRow(sheet, cell, &cells)
	}
	for _, it := range items {
		places := append([]store.StockPlace(nil), it.Places...)
		// The usual place leads, so that loading the file back keeps it as the home.
		sort.SliceStable(places, func(i, j int) bool {
			return places[i].LocationCode == it.Home && places[j].LocationCode != it.Home
		})
		if len(places) == 0 || (it.Home != "" && places[0].LocationCode != it.Home) {
			places = append([]store.StockPlace{{LocationCode: it.Home}}, places...)
		}
		for _, p := range places {
			if err := put(it, p.LocationCode, p.Qty); err != nil {
				return err
			}
		}
	}
	return f.Write(w)
}
