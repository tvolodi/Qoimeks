package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"qoimeks/server/internal/store"
)

const providerKey = "provider-key-0123456789"

type feedbackID struct {
	ID int64 `json:"id"`
}

func TestFeedbackNeedsToken(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	call(t, "POST", url+"/api/feedback", "", map[string]any{"text": "hi"}, 401, nil)
	call(t, "POST", url+"/api/feedback", "wrong", map[string]any{"text": "hi"}, 401, nil)
	var out feedbackID
	call(t, "POST", url+"/api/feedback", "shared",
		map[string]any{"text": "hi", "operator": "Аня", "screen": "history", "app_version": "0.2.0"}, 201, &out)
	rows, err := srv.Store.ListFeedback(t.Context(), 0, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows: %v %+v", err, rows)
	}
	if rows[0].ID != out.ID || out.ID == 0 || rows[0].Kind != "user" || rows[0].Text != "hi" ||
		rows[0].Operator != "Аня" || rows[0].Screen != "history" || string(rows[0].Errors) != "[]" {
		t.Fatalf("bad row: %+v (id %d)", rows[0], out.ID)
	}
}

func TestFeedbackRedactsSecrets(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared-access-token")
	srv.ProviderKeys = []string{providerKey, "short", ""}
	var tn tenantBody
	call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &tn)
	sk := "sk-" + strings.Repeat("a1B2_", 4)
	secrets := []string{"shared-access-token", providerKey, "Bearer abc123def456", sk}

	body := func(code string) map[string]any {
		all := strings.Join(append([]string{code}, secrets...), " | ")
		return map[string]any{
			"text":       "boom " + all,
			"voice_text": "said " + all,
			"operator":   "op " + code,
			"errors": []any{
				"plain " + all,
				map[string]any{"msg": all, "nested": []any{code, "x " + providerKey}, "n": 3},
			},
		}
	}
	check := func(label string, st *store.Store, code string) {
		t.Helper()
		rows, err := st.ListFeedback(t.Context(), 0, 10)
		if err != nil || len(rows) != 1 {
			t.Fatalf("%s: %v %+v", label, err, rows)
		}
		raw, _ := json.Marshal(rows[0])
		for _, s := range append([]string{code}, secrets...) {
			if strings.Contains(string(raw), s) {
				t.Errorf("%s: stored row contains %q: %s", label, s, raw)
			}
		}
		if !strings.Contains(string(raw), "[redacted]") || !strings.Contains(rows[0].Text, "boom") ||
			rows[0].VoiceText == nil || !strings.HasPrefix(*rows[0].VoiceText, "said ") {
			t.Errorf("%s: row not as expected: %s", label, raw)
		}
	}

	// A tenant request writes to the tenant's schema, not the default one.
	call(t, "POST", url+"/api/feedback", tn.AccessCode, body(tn.AccessCode), 201, nil)
	tenant, err := srv.Store.TenantByCode(t.Context(), tn.AccessCode)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := srv.tenantStore(t.Context(), tenant.SchemaName)
	if err != nil {
		t.Fatal(err)
	}
	check("tenant", ts, tn.AccessCode)
	if rows, _ := srv.Store.ListFeedback(t.Context(), 0, 10); len(rows) != 0 {
		t.Fatalf("tenant feedback landed in the default schema: %+v", rows)
	}

	call(t, "POST", url+"/api/feedback", "shared-access-token", body("shared-access-token"), 201, nil)
	check("default", srv.Store, "shared-access-token")
	if rows, _ := ts.ListFeedback(t.Context(), 0, 10); len(rows) != 1 {
		t.Fatalf("default feedback landed in the tenant schema: %+v", rows)
	}
}

func TestFeedbackLimits(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	errs := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "e"
		}
		return out
	}
	for name, c := range map[string]struct {
		body map[string]any
		want int
	}{
		"empty text":        {map[string]any{"text": ""}, 400},
		"blank text":        {map[string]any{"text": "  \n"}, 400},
		"missing text":      {map[string]any{"operator": "o"}, 400},
		"text 4001":         {map[string]any{"text": strings.Repeat("я", 4001)}, 400},
		"text 4000":         {map[string]any{"text": strings.Repeat("я", 4000)}, 201},
		"voice_text 4001":   {map[string]any{"text": "t", "voice_text": strings.Repeat("a", 4001)}, 400},
		"51 errors":         {map[string]any{"text": "t", "errors": errs(51)}, 400},
		"50 errors":         {map[string]any{"text": "t", "errors": errs(50)}, 201},
		"long device":       {map[string]any{"text": "t", "device": strings.Repeat("d", 201)}, 400},
		"voice_text stored": {map[string]any{"text": "t", "voice_text": "голос"}, 201},
	} {
		t.Run(name, func(t *testing.T) {
			call(t, "POST", url+"/api/feedback", "shared", c.body, c.want, nil)
		})
	}
	call(t, "POST", url+"/api/feedback", "shared", "not json", 400, nil)

	rows, err := srv.Store.ListFeedback(t.Context(), 0, 100)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows: %v %d", err, len(rows))
	}
	var voice bool
	for _, r := range rows {
		voice = voice || (r.VoiceText != nil && *r.VoiceText == "голос")
	}
	if !voice {
		t.Fatalf("voice_text not stored: %+v", rows)
	}
}

func TestFeedbackListAndExport(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	for _, p := range []string{"/api/feedback", "/api/feedback/export.md"} {
		call(t, "GET", url+p, "", nil, 401, nil)
		call(t, "GET", url+p, "wrong", nil, 401, nil)
	}
	var empty []store.Feedback
	call(t, "GET", url+"/api/feedback", "shared", nil, 200, &empty)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list: %#v", empty)
	}

	call(t, "POST", url+"/api/feedback", "shared", map[string]any{
		"text": "первый отзыв", "operator": "Аня", "screen": "history", "app_version": "0.2.0",
		"android_version": "13", "device": "Pixel 7", "errors": []any{"timeout one", map[string]any{"msg": "boom two"}},
	}, 201, nil)
	if _, err := srv.Store.AppendFeedback(t.Context(), store.Feedback{Kind: "llm_error", Text: "второй сбой", Operator: "Боря"}); err != nil {
		t.Fatal(err)
	}

	var rows []store.Feedback
	call(t, "GET", url+"/api/feedback", "shared", nil, 200, &rows)
	if len(rows) != 2 || rows[0].Text != "второй сбой" || rows[1].Text != "первый отзыв" || rows[0].ID <= rows[1].ID {
		t.Fatalf("not newest first: %+v", rows)
	}
	call(t, "GET", url+"/api/feedback?limit=1", "shared", nil, 200, &rows)
	if len(rows) != 1 || rows[0].Text != "второй сбой" {
		t.Fatalf("limit: %+v", rows)
	}

	req, _ := http.NewRequest("GET", url+"/api/feedback/export.md", nil)
	req.Header.Set("Authorization", "Bearer shared")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	md := string(raw)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") ||
		!strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", resp.StatusCode, resp.Header)
	}
	for _, want := range []string{"первый отзыв", "второй сбой", "user", "llm_error", "0.2.0", "13", "Pixel 7",
		"Аня", "Боря", "history", "2026-", "timeout one", "boom two"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Index(md, "второй сбой") > strings.Index(md, "первый отзыв") {
		t.Errorf("markdown not newest first:\n%s", md)
	}
}

func TestWebPageHasFeedbackSection(t *testing.T) {
	for _, want := range []string{`href="#feedback"`, `id="page-feedback"`, "Отзывы", "/api/feedback", "/api/feedback/export.md",
		"localStorage.getItem(FEEDBACK_SEEN)", "localStorage.setItem(FEEDBACK_SEEN", "новое", "feedbackKinds"} {
		if !strings.Contains(string(webPage), want) {
			t.Errorf("web.html lacks %q", want)
		}
	}
}
