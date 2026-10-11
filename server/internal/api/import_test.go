package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/xuri/excelize/v2"

	"qoimeks/server/internal/store"
)

func workbook(t *testing.T, rows [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// upload posts a workbook to an admin import route and decodes the answer.
func upload(t *testing.T, url, token string, file []byte, fields map[string]string, out any) int {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if file != nil {
		part, _ := w.CreateFormFile("file", "stock.xlsx")
		part.Write(file)
	}
	for k, v := range fields {
		w.WriteField(k, v)
	}
	w.Close()
	req, _ := http.NewRequest("POST", url, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestAdminImport(t *testing.T) {
	c, _, _ := setup(t, &Server{AccessToken: "phone"})
	file := workbook(t, [][]any{
		{"Артикул", "Номенклатура", "Конечный остаток"},
		{"Z-1", "Z-1 Замок", 10, "R", "I", 2, "RK2"},
		{"Z-2", "Z-2 Кнопка", 4, "L", "A", 1},
		{"Z-2", "Совсем другой товар", 9, "L", "A", 2},
	})
	preview, apply := c.url+"/api/import/preview", c.url+"/api/import/apply"
	withBalances := map[string]string{"balances": "true"}

	// Importing needs the access code like everything else under /api/.
	if got := upload(t, preview, "wrong", file, withBalances, nil); got != 401 {
		t.Fatalf("preview with a wrong code: %d", got)
	}
	if got := upload(t, apply, "", file, withBalances, nil); got != 401 {
		t.Fatalf("apply without a code: %d", got)
	}

	// A preview reports the changes and leaves the database alone.
	var p importResponse
	if got := upload(t, preview, "phone", file, withBalances, &p); got != 200 {
		t.Fatalf("preview: %d", got)
	}
	want := store.ImportResult{LocationsCreated: 4, SKUsCreated: 2, BalancesPosted: 2}
	if p.Applied || p.Changes != want || p.SKUs != 2 || len(p.Remarks) != 1 || len(p.Sheets) != 1 || p.SHA256 == "" {
		t.Fatalf("preview: %+v", p)
	}
	var cat store.Catalog
	c.do("GET", "/api/catalog", nil, 200, &cat)
	for _, s := range cat.SKUs {
		if s.Code == "Z-1" {
			t.Fatal("the preview changed the catalog")
		}
	}

	// Applying needs a name and the checksum of the file that was previewed.
	fields := map[string]string{"balances": "true", "sha256": p.SHA256}
	if got := upload(t, apply, "phone", file, fields, nil); got != 400 {
		t.Fatalf("apply without a name: %d", got)
	}
	fields["who"] = "Гульнара"
	other := workbook(t, [][]any{{"Артикул", "Номенклатура"}, {"Z-9", "Z-9 Подмена", 1, "C"}})
	if got := upload(t, apply, "phone", other, fields, nil); got != 409 {
		t.Fatalf("apply of a different file: %d", got)
	}
	var a importResponse
	if got := upload(t, apply, "phone", file, fields, &a); got != 200 || !a.Applied || a.Changes != want {
		t.Fatalf("apply: %d %+v", got, a)
	}

	// Loading the same file again changes no balance; both loads are in the history.
	if got := upload(t, apply, "phone", file, fields, &a); got != 200 || a.Changes != (store.ImportResult{SKUsUpdated: 2}) {
		t.Fatalf("second apply: %d %+v", got, a)
	}
	req, _ := http.NewRequest("GET", c.url+"/api/imports", nil)
	req.Header.Set("Authorization", "Bearer phone")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var history []store.ImportRecord
	json.NewDecoder(resp.Body).Decode(&history)
	if len(history) != 2 || history[1].Who != "Гульнара" || history[1].Filename != "stock.xlsx" ||
		history[1].ImportResult != want || !history[1].WithBalances || history[1].Remarks != 1 {
		t.Fatalf("history: %+v", history)
	}

	// Not a workbook at all.
	if got := upload(t, preview, "phone", []byte("hello"), nil, nil); got != 422 {
		t.Fatalf("garbage file: %d", got)
	}
}

func TestAdminPageMovedToTheRoot(t *testing.T) {
	c, _, _ := setup(t, &Server{})
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(c.url + "/admin/")
	if err != nil || resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != "/" {
		t.Fatalf("old admin address: %v %v", resp, err)
	}
	resp.Body.Close()
}
