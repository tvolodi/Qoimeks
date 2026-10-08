package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

type stubParser struct{ draft parse.Draft }

func (p stubParser) Parse(context.Context, store.Catalog, string) (parse.Draft, error) {
	return p.draft, nil
}

type stubTranscriber struct{ text string }

func (t stubTranscriber) Transcribe(context.Context, []byte, string, string) (string, error) {
	return t.text, nil
}

type client struct {
	t   *testing.T
	url string
}

func (c client) do(method, path string, body any, wantStatus int, out any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.url+path, &buf)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	json.NewDecoder(resp.Body).Decode(&raw)
	if resp.StatusCode != wantStatus {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, wantStatus, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: %v: %s", method, path, err, raw)
		}
	}
}

func setup(t *testing.T, srv *Server) (client, map[string]int64, map[string]int64) {
	srv.Store = storetest.New(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := client{t, ts.URL}
	var cat store.Catalog
	c.do("GET", "/api/catalog", nil, 200, &cat)
	skus, locs := map[string]int64{}, map[string]int64{}
	for _, s := range cat.SKUs {
		skus[s.Code] = s.ID
	}
	for _, l := range cat.Locations {
		locs[l.Code] = l.ID
	}
	return c, skus, locs
}

func balanceAt(m store.Movement, code string) float64 {
	for _, b := range m.Balances {
		if b.LocationCode == code {
			return b.Qty
		}
	}
	return -999999
}

func TestLedger(t *testing.T) {
	c, skus, locs := setup(t, &Server{})
	sku, a1, b1 := skus["NUT-M8"], locs["A-01"], locs["B-01"]
	post := func(kind string, loc int64, to *int64, qty float64, want int) store.Movement {
		t.Helper()
		var m store.Movement
		c.do("POST", "/api/movements", store.NewMovement{
			ID: uuid.New(), Kind: kind, SKUID: sku, LocationID: loc, ToLocationID: to, Qty: qty, Operator: "Айгуль",
		}, want, &m)
		return m
	}

	if got := balanceAt(post("receipt", a1, nil, 100, 201), "A-01"); got != 100 {
		t.Fatalf("after receipt: %v", got)
	}
	if got := balanceAt(post("issue", a1, nil, 30.5, 201), "A-01"); got != 69.5 {
		t.Fatalf("after issue: %v", got)
	}
	tr := post("transfer", a1, &b1, 20, 201)
	if balanceAt(tr, "A-01") != 49.5 || balanceAt(tr, "B-01") != 20 {
		t.Fatalf("after transfer: %+v", tr.Balances)
	}
	adj := post("adjustment", b1, nil, 18, 201)
	if adj.Balances[0].Qty != 18 || adj.Balances[0].Delta != -2 {
		t.Fatalf("after count: %+v", adj.Balances)
	}
	// Issuing more than is there is allowed; the balance simply goes negative.
	if got := balanceAt(post("issue", b1, nil, 25, 201), "B-01"); got != -7 {
		t.Fatalf("after over-issue: %v", got)
	}

	// Rejected input.
	post("receipt", a1, nil, 0, 400)
	post("receipt", a1, &b1, 1, 400)
	post("transfer", a1, nil, 1, 400)
	post("transfer", a1, &a1, 1, 400)
	post("receipt", 999999, nil, 1, 400)
	post("bogus", a1, nil, 1, 400)

	// Posting the same ID again changes nothing.
	again := store.NewMovement{ID: uuid.New(), Kind: "receipt", SKUID: sku, LocationID: a1, Qty: 5, Operator: "Айгуль"}
	var first, second store.Movement
	c.do("POST", "/api/movements", again, 201, &first)
	c.do("POST", "/api/movements", again, 200, &second)
	if first.Seq != second.Seq || balanceAt(second, "A-01") != 54.5 {
		t.Fatalf("retry was not idempotent: %+v", second)
	}

	// The same ID with different data is a mistake, not a retry.
	again.Qty = 6
	c.do("POST", "/api/movements", again, 409, nil)

	// Reversing the transfer puts both locations back; a second reversal is refused.
	var rev store.Movement
	revBody := map[string]any{"id": uuid.New(), "operator": "Айгуль"}
	c.do("POST", "/api/movements/"+tr.ID.String()+"/reverse", revBody, 201, &rev)
	if balanceAt(rev, "A-01") != 74.5 || balanceAt(rev, "B-01") != -27 {
		t.Fatalf("after reversal: %+v", rev.Balances)
	}
	c.do("POST", "/api/movements/"+tr.ID.String()+"/reverse", revBody, 200, nil)
	c.do("POST", "/api/movements/"+tr.ID.String()+"/reverse", map[string]any{"id": uuid.New(), "operator": "Айгуль"}, 400, nil)
	c.do("POST", "/api/movements/"+rev.ID.String()+"/reverse", map[string]any{"id": uuid.New(), "operator": "Айгуль"}, 400, nil)

	var current []store.Balance
	c.do("GET", "/api/balances?sku_id="+itoa(sku), nil, 200, &current)
	if len(current) != 2 || current[0].Qty != 74.5 || current[1].Qty != -27 {
		t.Fatalf("current balances: %+v", current)
	}
	var history []store.Movement
	c.do("GET", "/api/movements", nil, 200, &history)
	if len(history) != 7 || history[0].Reverses == nil || *history[0].Reverses != tr.ID {
		t.Fatalf("history: %d rows, newest %+v", len(history), history[0])
	}
}

func itoa(v int64) string { b, _ := json.Marshal(v); return string(b) }

func TestConcurrentPostsKeepRunningBalance(t *testing.T) {
	c, skus, locs := setup(t, &Server{})
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.do("POST", "/api/movements", store.NewMovement{
				ID: uuid.New(), Kind: "receipt", SKUID: skus["WSH-8"], LocationID: locs["C-01"], Qty: 1, Operator: "t",
			}, 201, nil)
		}()
	}
	wg.Wait()
	var current []store.Balance
	c.do("GET", "/api/balances?sku_id="+itoa(skus["WSH-8"]), nil, 200, &current)
	if len(current) != 1 || current[0].Qty != 40 {
		t.Fatalf("balance after 40 concurrent receipts: %+v", current)
	}
}

func TestVoiceDraftIsCheckedAgainstCatalog(t *testing.T) {
	qty := 15.0
	c, _, _ := setup(t, &Server{
		Transcriber: stubTranscriber{"переместил пятнадцать гаек из а один в бэ два"},
		Parser: stubParser{parse.Draft{
			Kind: "transfer", SKUCodes: []string{"NUT-M8", "NO-SUCH", "NUT-M10", "NUT-M8"},
			LocationCode: "A-01", ToLocationCode: "B-02", Qty: &qty,
		}},
	})
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("audio", "phrase.m4a")
	part.Write([]byte("not really audio"))
	w.Close()
	resp, err := http.Post(c.url+"/api/voice", w.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var d DraftResponse
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil || resp.StatusCode != 200 {
		t.Fatalf("status %d, err %v", resp.StatusCode, err)
	}
	if d.Kind != "transfer" || len(d.SKUs) != 2 || d.SKUs[0].Code != "NUT-M8" || d.SKUs[1].Code != "NUT-M10" ||
		d.Location == nil || d.Location.Code != "A-01" || d.ToLocation == nil || d.ToLocation.Code != "B-02" ||
		d.Qty == nil || *d.Qty != 15 || d.Transcript == "" {
		t.Fatalf("draft: %+v", d)
	}
}

func TestVoiceWithoutServicesSaysSo(t *testing.T) {
	c, _, _ := setup(t, &Server{})
	c.do("POST", "/api/parse", map[string]string{"text": "приход"}, 503, nil)
	c.do("POST", "/api/voice", nil, 503, nil)
}
