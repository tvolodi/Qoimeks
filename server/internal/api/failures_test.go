package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

const failKeyText = "failing-provider-key-9876543210"

type failingParser struct{}

func (failingParser) Parse(context.Context, store.Catalog, string) (parse.Draft, error) {
	return parse.Draft{}, errors.New("llm down, key " + failKeyText)
}

type failingTranscriber struct{}

func (failingTranscriber) Transcribe(context.Context, []byte, string, string) (string, error) {
	return "", errors.New("stt down, Bearer " + failKeyText)
}

func failureSetup(t *testing.T) (*Server, string, *time.Time) {
	srv, url, clock := adminSetup(t, "shared")
	srv.Parser, srv.LLMName = failingParser{}, "fakellm"
	srv.Transcriber, srv.STTName = failingTranscriber{}, "fakestt"
	srv.ProviderKeys = []string{failKeyText}
	// Posting fails inside the database, as a lost connection or a full disk would.
	storetest.Exec(t, srv.Store, `create function fail_insert() returns trigger language plpgsql as $$ begin raise exception 'disk full'; end $$;
		create trigger fail_insert before insert on movements for each row execute function fail_insert()`)
	return srv, url, clock
}

func send(t *testing.T, method, url, token string, header map[string]string, contentType string, body []byte, want int) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("%s %s: status %d, want %d", method, url, resp.StatusCode, want)
	}
}

func audioBody() ([]byte, string) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("audio", "phrase.m4a")
	part.Write([]byte("not really audio"))
	w.Close()
	return body.Bytes(), w.FormDataContentType()
}

func badMovement(t *testing.T, kind string, sku int64) []byte {
	b, err := json.Marshal(store.NewMovement{ID: uuid.New(), Kind: kind, SKUID: sku, LocationID: 1, Qty: 1, Operator: "Аня"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var appHeaders = map[string]string{"X-App-Version": "0.3.1%2B7", "X-Device": "Samsung%20A14", "X-Operator": "%D0%90%D0%BD%D1%8F"}

func failureRows(t *testing.T, st *store.Store) []store.Feedback {
	t.Helper()
	rows, err := st.ListFeedback(t.Context(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestProviderAndStoreFailuresAreRecorded(t *testing.T) {
	srv, url, _ := failureSetup(t)
	audio, ct := audioBody()
	send(t, "POST", url+"/api/voice", "shared", appHeaders, ct, audio, 502)
	send(t, "POST", url+"/api/parse", "shared", appHeaders, "application/json", []byte(`{"text":"приход гайки"}`), 502)
	send(t, "POST", url+"/api/movements", "shared", appHeaders, "application/json", badMovement(t, "receipt", 1), 500)

	rows := failureRows(t, srv.Store) // oldest first
	want := []struct{ kind, provider string }{{"speech_error", "fakestt"}, {"llm_error", "fakellm"}, {"post_error", "store"}}
	if len(rows) != len(want) {
		t.Fatalf("rows: %+v", rows)
	}
	for i, w := range want {
		r := rows[i]
		if r.Kind != w.kind || r.Provider == nil || *r.Provider != w.provider {
			t.Errorf("row %d: %+v", i, r)
		}
		if r.AppVersion != "0.3.1+7" || r.Device != "Samsung A14" || r.Operator != "Аня" || r.AndroidVersion != "" || r.Screen != "" {
			t.Errorf("row %d details: %+v", i, r)
		}
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), failKeyText) || strings.Contains(string(raw), "shared") {
			t.Errorf("row %d leaks a secret: %s", i, raw)
		}
		if !strings.Contains(string(r.Errors), `"class"`) || !strings.Contains(string(r.Errors), `"message"`) {
			t.Errorf("row %d errors: %s", i, r.Errors)
		}
	}
	if !strings.Contains(rows[0].Text, "[redacted]") || !strings.Contains(rows[1].Text, "[redacted]") {
		t.Errorf("secrets not redacted: %q %q", rows[0].Text, rows[1].Text)
	}
}

func TestFailureWindowDedupesWithoutUpdates(t *testing.T) {
	srv, url, clock := failureSetup(t)
	post := func() {
		send(t, "POST", url+"/api/parse", "shared", nil, "application/json", []byte(`{"text":"приход"}`), 502)
	}
	post()
	*clock = clock.Add(59 * time.Second)
	post()
	if rows := failureRows(t, srv.Store); len(rows) != 1 {
		t.Fatalf("within the window: %d rows", len(rows))
	}
	// A different kind is its own key.
	audio, ct := audioBody()
	send(t, "POST", url+"/api/voice", "shared", nil, ct, audio, 502)
	*clock = clock.Add(2 * time.Second) // 61 s after the first parse failure
	post()
	rows := failureRows(t, srv.Store)
	if len(rows) != 3 {
		t.Fatalf("after the window: %d rows", len(rows))
	}
	// Rows are only ever inserted: the first one is untouched.
	if rows[0].Kind != "llm_error" || rows[1].Kind != "speech_error" || rows[2].Kind != "llm_error" {
		t.Fatalf("rows: %+v", rows)
	}
}

func TestFailureRowsWithoutHeadersAndValidationErrors(t *testing.T) {
	srv, url, _ := failureSetup(t)
	send(t, "POST", url+"/api/parse", "shared", nil, "application/json", []byte(`{"text":"приход"}`), 502)
	rows := failureRows(t, srv.Store)
	if len(rows) != 1 || rows[0].AppVersion != "" || rows[0].Device != "" || rows[0].Operator != "" {
		t.Fatalf("rows: %+v", rows)
	}
	// 400s: bad JSON, missing audio, invalid movement; and a nothing-said 422.
	send(t, "POST", url+"/api/movements", "shared", nil, "application/json", []byte(`{`), 400)
	send(t, "POST", url+"/api/movements", "shared", nil, "application/json", badMovement(t, "bogus", 1), 400)
	send(t, "POST", url+"/api/voice", "shared", nil, "multipart/form-data; boundary=x", []byte("--x--"), 400)
	send(t, "POST", url+"/api/parse", "shared", nil, "application/json", []byte(`{"text":"  "}`), 422)
	if rows := failureRows(t, srv.Store); len(rows) != 1 {
		t.Fatalf("validation errors created rows: %+v", rows)
	}
}

func TestTenantFailureWritesIntoTenantSchema(t *testing.T) {
	srv, url, _ := failureSetup(t)
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &tn)
	send(t, "POST", url+"/api/parse", tn.AccessCode, appHeaders, "application/json", []byte(`{"text":"приход"}`), 502)

	tenant, err := srv.Store.TenantByCode(t.Context(), tn.AccessCode)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := srv.tenantStore(t.Context(), tenant.SchemaName)
	if err != nil {
		t.Fatal(err)
	}
	if rows := failureRows(t, ts); len(rows) != 1 || rows[0].Kind != "llm_error" {
		t.Fatalf("tenant rows: %+v", rows)
	}
	if rows := failureRows(t, srv.Store); len(rows) != 0 {
		t.Fatalf("default schema got the tenant's failure: %+v", rows)
	}
	// The dedupe window is per schema: the default schema's first failure is still recorded.
	send(t, "POST", url+"/api/parse", "shared", nil, "application/json", []byte(`{"text":"приход"}`), 502)
	if rows := failureRows(t, srv.Store); len(rows) != 1 {
		t.Fatalf("default rows: %+v", rows)
	}
}

// errorSource posts and returns the "source" field of the JSON error body.
func errorSource(t *testing.T, url, contentType string, body []byte, want int) string {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer shared")
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("%s: status %d, want %d", url, resp.StatusCode, want)
	}
	var out struct{ Error, Source string }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error == "" {
		t.Errorf("%s: no error text", url)
	}
	return out.Source
}

func TestProviderErrorBodiesNameTheFailedPart(t *testing.T) {
	srv, url, _ := failureSetup(t)
	audio, ct := audioBody()
	if got := errorSource(t, url+"/api/voice", ct, audio, 502); got != "speech" {
		t.Errorf("voice failure source %q", got)
	}
	if got := errorSource(t, url+"/api/parse", "application/json", []byte(`{"text":"приход гайки"}`), 502); got != "llm" {
		t.Errorf("parse failure source %q", got)
	}
	// Not configured counts as unavailable too.
	srv.Parser = nil
	if got := errorSource(t, url+"/api/parse", "application/json", []byte(`{"text":"приход гайки"}`), 503); got != "llm" {
		t.Errorf("parse unconfigured source %q", got)
	}
	srv.Transcriber = nil
	if got := errorSource(t, url+"/api/voice", ct, audio, 503); got != "speech" {
		t.Errorf("voice unconfigured source %q", got)
	}
}
