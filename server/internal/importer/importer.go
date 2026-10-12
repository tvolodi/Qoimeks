// Package importer reads the bookkeepers' weekly stock workbook.
//
// Each sheet is one week (the first sheet is the newest). After a header row
// starting with "Артикул" the columns are:
//
//	A article   B name   C closing balance
//	D zone (L left, R right, D straight ahead, C and others)   E rack letter   F shelf number
//	G another place to look, written together, e.g. "RK2"
//
// An article may take several rows: one per place that holds it, and now and then
// the same place twice. Its rows are added up by place, because the ledger keeps
// one balance per article and place.
//
// The file is kept by hand, so everything doubtful is reported instead of guessed.
// Remarks are in Russian: their readers are the bookkeepers who fix the file.
package importer

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"

	"qoimeks/server/internal/store"
)

// NoLocation holds stock whose row gives a balance but no place.
const NoLocation = "?"

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

// quantity reads a balance cell. Excel stores a number plainly, but a balance pasted
// as text keeps the look its program gave it: a decimal comma or dot, thousands
// split by spaces, apostrophes, or the other of the two signs.
func quantity(raw string) (float64, bool) {
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\'' || r == '’' {
			return -1
		}
		return r
	}, raw)
	commas, dots := strings.Count(s, ","), strings.Count(s, ".")
	decimal := ""
	switch {
	case commas > 0 && dots > 0: // the sign written last is the decimal one
		decimal = ","
		if strings.LastIndex(s, ".") > strings.LastIndex(s, ",") {
			decimal = "."
		}
	case commas == 1:
		decimal = ","
	case dots == 1:
		decimal = "."
	}
	for _, sign := range []string{",", "."} {
		if sign != decimal {
			s = strings.ReplaceAll(s, sign, "")
		}
	}
	if decimal != "" {
		s = strings.ReplaceAll(s, decimal, ".")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
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
		unclear := false
		if raw := cell(2); raw != "" {
			v, ok := quantity(raw)
			if !ok {
				unclear = true
				warn(n, "%s: остаток %q не является количеством. Остатки этого товара в системе не изменяются", code, raw)
			}
			qty = v
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

		sku := store.ImportSKU{Code: code, Name: name, Home: home, Alt: alt, KeepBalances: unclear}
		if home != "" && !unclear {
			sku.Stock = []store.ImportStock{{Location: home, Qty: qty}}
		}
		// A name that begins with the article is the bookkeepers' own sign of which goods a row means.
		namedByCode := func(name string) bool { return strings.HasPrefix(strings.ToUpper(name), strings.ToUpper(code)) }

		if idx, seen := bySKU[code]; seen {
			first := &res.SKUs[idx]
			first.KeepBalances = first.KeepBalances || unclear
			same := strings.EqualFold(first.Name, name) || (namedByCode(first.Name) && namedByCode(name))
			if !same {
				other := name
				if !namedByCode(first.Name) && namedByCode(name) {
					// The earlier row carries this article by mistake; this row is the real one.
					other, first.Name = first.Name, name
					if home != "" {
						home, first.Home = first.Home, home
					}
				}
				warn(n, "%s: артикул повторяется с другим названием %q. Остатки сложены, товар назван %q", code, other, first.Name)
			}
			if home != "" {
				if first.Home == "" {
					first.Home = home
				} else if first.Alt == "" && home != first.Home {
					first.Alt = home
				}
			}
			for _, st := range sku.Stock {
				at := -1
				for j := range first.Stock {
					if first.Stock[j].Location == st.Location {
						at = j
					}
				}
				if at < 0 {
					first.Stock = append(first.Stock, st)
					continue
				}
				first.Stock[at].Qty += st.Qty
				if same {
					warn(n, "%s: место %s указано повторно, остатки сложены: %v", code, st.Location, first.Stock[at].Qty)
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
