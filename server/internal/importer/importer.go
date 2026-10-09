// Package importer reads the bookkeepers' weekly stock workbook.
//
// Each sheet is one week (the first sheet is the newest). After a header row
// starting with "Артикул" the columns are:
//
//	A article   B name   C closing balance
//	D zone (L left, R right, D straight ahead, C and others)   E rack letter   F shelf number
//	G another place to look, written together, e.g. "RK2"
//
// The file is kept by hand, so everything doubtful is reported instead of guessed.
// Remarks are in Russian: their readers are the bookkeepers who fix the file.
package importer

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"qoimeks/server/internal/store"
)

// NoLocation holds stock whose row gives a balance but no place.
const NoLocation = "?"

// Skipped starts every remark about a row that was left out of the import.
const Skipped = "НЕ ЗАГРУЖЕНО"

type Result struct {
	Sheet     string
	Sheets    []string // all sheets of the workbook, newest first
	Locations []store.ImportLocation
	SKUs      []store.ImportSKU
	Warnings  []string
}

var zoneNames = map[string]string{"L": "Слева", "R": "Справа", "D": "Прямо"}

// Cyrillic letters that look like Latin ones, typed on the wrong keyboard layout.
var lookalikes = strings.NewReplacer(
	"А", "A", "В", "B", "С", "C", "Е", "E", "Н", "H", "К", "K", "М", "M", "О", "O", "Р", "P", "Т", "T", "Х", "X")

var (
	spaces   = regexp.MustCompile(`\s+`)
	altShape = regexp.MustCompile(`^([A-Z])([A-Z]?)(\d*)$`)
)

func clean(s string) string { return spaces.ReplaceAllString(strings.TrimSpace(s), " ") }

// letter normalises a zone or rack cell to an upper-case Latin letter.
func letter(s string) string {
	s = lookalikes.Replace(strings.ToUpper(clean(s)))
	if s == "0" { // a zero typed instead of the letter O
		return "O"
	}
	return s
}

func stockTotal(stock []store.ImportStock) float64 {
	total := 0.0
	for _, s := range stock {
		total += s.Qty
	}
	return total
}

func locationName(zone, rack, shelf string) string {
	name := zoneNames[zone]
	if name == "" || rack == "" {
		return name
	}
	return strings.TrimSpace(name + " " + rack + " " + shelf)
}

// Parse reads one sheet of the workbook at path; an empty sheet name means the
// first (newest) one.
func Parse(path, sheet string) (*Result, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(f, sheet)
}

// ParseReader is Parse for a workbook held in memory.
func ParseReader(r io.Reader, sheet string) (*Result, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parse(f, sheet)
}

func parse(f *excelize.File, sheet string) (*Result, error) {
	sheets := f.GetSheetList()
	if sheet == "" {
		sheet = sheets[0]
	}
	rows, err := f.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, err
	}

	res := &Result{Sheet: sheet, Sheets: sheets}
	warn := func(row int, format string, a ...any) {
		res.Warnings = append(res.Warnings, fmt.Sprintf("строка %d: %s", row, fmt.Sprintf(format, a...)))
	}
	locations := map[string]bool{}
	addLocation := func(zone, rack, shelf string) string {
		code := zone + rack + shelf
		if !locations[code] {
			locations[code] = true
			res.Locations = append(res.Locations, store.ImportLocation{Code: code, Name: locationName(zone, rack, shelf)})
		}
		return code
	}
	bySKU := map[string]int{} // code -> index in res.SKUs

	started := false
	for i, r := range rows {
		n := i + 1
		cell := func(c int) string {
			if c < len(r) {
				return clean(r[c])
			}
			return ""
		}
		if !started {
			started = cell(0) == "Артикул"
			continue
		}
		code, name := cell(0), cell(1)
		if code == "" || name == "" {
			continue // blank line or a group heading such as "Основной"
		}

		var qty float64
		if raw := cell(2); raw != "" {
			v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
			if err != nil || v < 0 {
				warn(n, "%s: остаток %q не является количеством, принят за 0", code, raw)
			} else {
				qty = v
			}
		}

		rawZone, rawRack, shelf := cell(3), cell(4), cell(5)
		zone, rack := letter(rawZone), letter(rawRack)
		if zone != strings.ToUpper(rawZone) || rack != strings.ToUpper(rawRack) {
			warn(n, "%s: место %q %q %s прочитано как %s%s%s", code, rawZone, rawRack, shelf, zone, rack, shelf)
		}
		home := ""
		switch {
		case len(zone) == 1 && zone >= "A" && zone <= "Z":
			home = addLocation(zone, rack, shelf)
		case zone != "" || rack != "" || shelf != "":
			warn(n, "%s: в месте %q %q %q не указана зона, место пропущено", code, rawZone, rawRack, shelf)
		}
		if home == "" && qty > 0 {
			home = addLocation(NoLocation, "", "")
			warn(n, "%s: остаток %v без места, записан в место %q", code, qty, NoLocation)
		}

		alt := ""
		if raw := cell(6); raw != "" {
			if m := altShape.FindStringSubmatch(lookalikes.Replace(strings.ToUpper(strings.ReplaceAll(raw, " ", "")))); m != nil {
				alt = addLocation(m[1], m[2], m[3])
			} else {
				warn(n, "%s: второе место %q не похоже на код места, пропущено", code, raw)
			}
		}

		sku := store.ImportSKU{Code: code, Name: name, Home: home, Alt: alt}
		if home != "" {
			sku.Stock = []store.ImportStock{{Location: home, Qty: qty}}
		}
		// A name that begins with the article is the bookkeepers' own sign of which goods a row means.
		namedByCode := func(name string) bool { return strings.HasPrefix(strings.ToUpper(name), strings.ToUpper(code)) }

		if idx, seen := bySKU[code]; seen {
			first := &res.SKUs[idx]
			same := strings.EqualFold(first.Name, name) || (namedByCode(first.Name) && namedByCode(name))
			if !same && namedByCode(name) {
				// The earlier row carries this article by mistake; this row is the real one.
				warn(n, "%s: %s: %q (остаток %v). Этот артикул принадлежит товару %q", code, Skipped, first.Name, stockTotal(first.Stock), name)
				*first = sku
				continue
			}
			if !same {
				warn(n, "%s: %s: %q (остаток %v). Этот артикул уже занят товаром %q", code, Skipped, name, qty, first.Name)
				continue
			}
			// The same article listed again: stock kept in a second place.
			if home != "" {
				first.Stock = append(first.Stock, store.ImportStock{Location: home, Qty: qty})
				if first.Home == "" {
					first.Home = home
				} else if first.Alt == "" && home != first.Home {
					first.Alt = home
				}
			}
			continue
		}
		bySKU[code] = len(res.SKUs)
		res.SKUs = append(res.SKUs, sku)
	}
	if !started {
		return nil, fmt.Errorf("на листе %q нет строки заголовка, начинающейся с «Артикул»", sheet)
	}
	return res, nil
}
