package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/providers"
	"qoimeks/server/internal/secrets"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
	"qoimeks/server/internal/stt"
)

const (
	sharedToken = "shared-access-token"
	apiKey      = "sk-test-0123456789abcdef-WXYZ"
)

type fakeProviders struct {
	llmErr, sttErr error
	calls          int
}

type fakeParser struct{ f *fakeProviders }

func (p fakeParser) Parse(context.Context, store.Catalog, string) (parse.Draft, error) {
	p.f.calls++
	return parse.Draft{}, p.f.llmErr
}

type fakeSTT struct{ f *fakeProviders }

func (t fakeSTT) Transcribe(_ context.Context, audio []byte, _, _ string) (string, error) {
	t.f.calls++
	if len(audio) == 0 {
		return "", errors.New("empty audio")
	}
	return "", t.f.sttErr
}

func (f *fakeProviders) LLM(context.Context) (parse.Parser, string, error) {
	return fakeParser{f}, "fake", nil
}
func (f *fakeProviders) Transcriber(context.Context) (stt.Transcriber, string, error) {
	return fakeSTT{f}, "fake", nil
}
func (f *fakeProviders) Secrets(context.Context) []string { return []string{apiKey} }

func settingsServer(t *testing.T, accessToken string, f *fakeProviders) (*Server, string) {
	t.Helper()
	st := storetest.New(t)
	k, err := secrets.ParseKey(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	srv := &Server{
		Store: st, AccessToken: accessToken, ProvisioningToken: provToken,
		Providers: f, Settings: &providers.Service{Store: st, Key: k}, SettingsUI: true,
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return srv, ts.URL
}

func TestSettingsAccess(t *testing.T) {
	put := map[string]any{"kind": "llm", "provider": "openai", "model": "m", "api_key": apiKey}

	t.Run("no token", func(t *testing.T) {
		srv, url := settingsServer(t, sharedToken, &fakeProviders{})
		call(t, "GET", url+"/api/settings/providers", "", nil, 401, nil)
		call(t, "PUT", url+"/api/settings/providers", "", put, 401, nil)
		call(t, "POST", url+"/api/settings/providers/test", "", map[string]string{"kind": "llm"}, 401, nil)
		if rows, _ := srv.Store.ListProviderSettings(t.Context()); len(rows) != 0 {
			t.Fatalf("stored %d rows", len(rows))
		}
	})
	t.Run("tenant code", func(t *testing.T) {
		srv, url := settingsServer(t, sharedToken, &fakeProviders{})
		var tn tenantBody
		call(t, "POST", url+"/admin/v1/tenants", provToken, newBody(), 201, &tn)
		call(t, "GET", url+"/api/settings/providers", tn.AccessCode, nil, 403, nil)
		call(t, "PUT", url+"/api/settings/providers", tn.AccessCode, put, 403, nil)
		call(t, "POST", url+"/api/settings/providers/test", tn.AccessCode, map[string]string{"kind": "llm"}, 403, nil)
		if rows, _ := srv.Store.ListProviderSettings(t.Context()); len(rows) != 0 {
			t.Fatalf("stored %d rows", len(rows))
		}
	})
	t.Run("no ACCESS_TOKEN", func(t *testing.T) {
		f := &fakeProviders{}
		srv, url := settingsServer(t, "", f)
		call(t, "GET", url+"/api/settings/providers", "", nil, 403, nil)
		call(t, "PUT", url+"/api/settings/providers", "", put, 403, nil)
		call(t, "POST", url+"/api/settings/providers/test", "", map[string]string{"kind": "llm"}, 403, nil)
		if rows, _ := srv.Store.ListProviderSettings(t.Context()); len(rows) != 0 || f.calls != 0 {
			t.Fatalf("stored %d rows, %d provider calls", len(rows), f.calls)
		}
	})
	t.Run("shared token", func(t *testing.T) {
		_, url := settingsServer(t, sharedToken, &fakeProviders{})
		var a providerSettingsAnswer
		call(t, "GET", url+"/api/settings/providers", sharedToken, nil, 200, &a)
		if len(a.LLMProviders) == 0 || len(a.STTProviders) != 2 {
			t.Fatalf("answer = %+v", a)
		}
	})
}

func TestSettingsSaveStoresEncryptedAndMasks(t *testing.T) {
	srv, url := settingsServer(t, sharedToken, &fakeProviders{})
	var a providerSettingsAnswer
	call(t, "PUT", url+"/api/settings/providers", sharedToken,
		map[string]any{"kind": "llm", "provider": "openai", "model": "gpt-x", "api_key": apiKey}, 200, &a)
	row, err := srv.Store.GetProviderSetting(t.Context(), "llm")
	if err != nil {
		t.Fatal(err)
	}
	if len(row.KeyEnc) == 0 || bytes.Contains(row.KeyEnc, []byte(apiKey)) {
		t.Fatalf("key not stored encrypted: %q", row.KeyEnc)
	}
	// GET and the PUT answer both show only the masked form.
	for _, path := range []string{"PUT", "GET"} {
		req, _ := http.NewRequest(path, url+"/api/settings/providers", strings.NewReader(`{"kind":"llm","provider":"openai","model":"gpt-x","keep_key":true}`))
		req.Header.Set("Authorization", "Bearer "+sharedToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || strings.Contains(string(body), apiKey) || !strings.Contains(string(body), "••••WXYZ") {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	if len(a.Settings) != 1 || !a.Settings[0].HasKey || a.Settings[0].Model != "gpt-x" {
		t.Fatalf("settings = %+v", a.Settings)
	}
	call(t, "PUT", url+"/api/settings/providers", sharedToken, map[string]any{"kind": "llm", "provider": "nope"}, 400, nil)
}

func TestSettingsTestButton(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	f := &fakeProviders{}
	_, url := settingsServer(t, sharedToken, f)
	var ans testAnswer
	for _, kind := range []string{"llm", "stt"} {
		call(t, "POST", url+"/api/settings/providers/test", sharedToken, map[string]string{"kind": kind}, 200, &ans)
		if !ans.OK {
			t.Fatalf("%s: %+v", kind, ans)
		}
	}
	if f.calls != 2 {
		t.Fatalf("provider calls = %d", f.calls)
	}

	f.llmErr = errors.New("401 invalid key " + apiKey)
	f.sttErr = errors.New("quota exceeded for " + apiKey)
	for _, kind := range []string{"llm", "stt"} {
		var raw map[string]any
		call(t, "POST", url+"/api/settings/providers/test", sharedToken, map[string]string{"kind": kind}, 200, &raw)
		msg, _ := raw["message"].(string)
		if raw["ok"] != false || msg == "" || strings.Contains(msg, apiKey) {
			t.Fatalf("%s: %+v", kind, raw)
		}
	}
	if strings.Contains(logs.String(), apiKey) {
		t.Fatalf("key in log: %s", logs.String())
	}
	call(t, "POST", url+"/api/settings/providers/test", sharedToken, map[string]string{"kind": "x"}, 400, nil)
}

func TestSettingsUIOffByDefault(t *testing.T) {
	st := storetest.New(t)
	srv := &Server{
		Store: st, AccessToken: sharedToken,
		Providers: &fakeProviders{}, Settings: &providers.Service{Store: st},
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	for _, bad := range []string{`page-settings`, `href="#settings"`, "/api/settings", "settings:begin", "settings:end", "loadSettings", "'settings'", "<h1>Настройки</h1>"} {
		if bytes.Contains(page, []byte(bad)) {
			t.Errorf("served page contains %q", bad)
		}
	}
	for _, want := range []string{`id="page-feedback"`, `href="#feedback"`, "const pages = ['balances', 'excel', 'history', 'feedback'];", "В приложении на телефоне нажмите значок шестерёнки"} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("served page lost %q", want)
		}
	}

	put := map[string]any{"kind": "llm", "provider": "openai", "model": "m", "api_key": apiKey}
	call(t, "GET", ts.URL+"/api/settings/providers", sharedToken, nil, 404, nil)
	call(t, "PUT", ts.URL+"/api/settings/providers", sharedToken, put, 404, nil)
	call(t, "POST", ts.URL+"/api/settings/providers/test", sharedToken, map[string]string{"kind": "llm"}, 404, nil)
	if rows, _ := st.ListProviderSettings(t.Context()); len(rows) != 0 {
		t.Fatalf("stored %d rows", len(rows))
	}
}

func TestSettingsUIOnServesPage(t *testing.T) {
	_, url := settingsServer(t, sharedToken, &fakeProviders{})
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(resp.Body)
	for _, want := range []string{`id="page-settings"`, `href="#settings"`, "loadSettings", "'settings'"} {
		if !bytes.Contains(page, []byte(want)) {
			t.Errorf("served page lacks %q", want)
		}
	}
}

func TestSettingsPageHasSection(t *testing.T) {
	for _, want := range []string{`id="page-settings"`, `href="#settings"`, "/api/settings/providers"} {
		if !bytes.Contains(webPage, []byte(want)) {
			t.Errorf("web.html lacks %s", want)
		}
	}
}
