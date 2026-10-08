package parse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qoimeks/server/internal/store"
)

var testCatalog = store.Catalog{
	SKUs:      []store.SKU{{ID: 1, Code: "ST-EL350MLD", Name: "ST-EL350MLD Замок электромагнитный", Unit: "шт"}},
	Locations: []store.Location{{ID: 7, Code: "LA3", Name: "Слева A 3"}},
}

// fakeChat answers every chat-completions call with content and records the request.
func fakeChat(t *testing.T, content string, got *map[string]any) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("unexpected request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(got)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
		})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestOpenAICompatibleReadsAnswersInEveryCommonShape(t *testing.T) {
	const object = `{"kind":"receipt","sku_codes":["ST-EL350MLD"],"location_words":"слева а три","location_code":"LA3","to_location_words":"","to_location_code":"","qty":80}`
	answers := map[string]string{
		"bare":      object,
		"fenced":    "```json\n" + object + "\n```",
		"reasoning": "<think>The operator received {something}...</think>\n" + object,
		"chatty":    "Here is the result:\n" + object + "\nLet me know if you need anything else.",
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			var req map[string]any
			ts := fakeChat(t, answer, &req)
			p, _, err := New(Settings{Provider: "custom", APIKey: "secret", BaseURL: ts.URL + "/", Model: "some-model"})
			if err != nil {
				t.Fatal(err)
			}
			d, err := p.Parse(context.Background(), testCatalog, "приход замок восемьдесят штук слева а три")
			if err != nil {
				t.Fatal(err)
			}
			if d.Kind != "receipt" || len(d.SKUCodes) != 1 || d.SKUCodes[0] != "ST-EL350MLD" || d.LocationCode != "LA3" || d.Qty == nil || *d.Qty != 80 {
				t.Fatalf("draft: %+v", d)
			}
			if req["model"] != "some-model" || req["response_format"] != nil {
				t.Fatalf("request: %v", req)
			}
			system := req["messages"].([]any)[0].(map[string]any)["content"].(string)
			if !strings.Contains(system, "ST-EL350MLD\tST-EL350MLD Замок электромагнитный") || !strings.Contains(system, "JSON object") {
				t.Fatalf("system prompt lacks the catalog or the JSON instructions")
			}
		})
	}
}

func TestProvidersAskForJSONTheWayEachSupports(t *testing.T) {
	for provider, want := range map[string]string{"openai": "json_schema", "deepseek": "json_object", "minimax": ""} {
		var req map[string]any
		ts := fakeChat(t, `{"kind":"unknown","sku_codes":[],"location_code":"","to_location_code":"","qty":null}`, &req)
		p, label, err := New(Settings{Provider: provider, APIKey: "secret", BaseURL: ts.URL})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Parse(context.Background(), testCatalog, "что-то"); err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		got := ""
		if rf, ok := req["response_format"].(map[string]any); ok {
			got = rf["type"].(string)
		}
		if got != want || req["model"] != Providers[provider].Model || !strings.HasPrefix(label, provider+" ") {
			t.Fatalf("%s: response_format %q, model %v, label %q", provider, got, req["model"], label)
		}
	}
}

func TestBadSettingsAndAnswersAreErrors(t *testing.T) {
	for _, s := range []Settings{
		{Provider: "nobody", APIKey: "k"},
		{Provider: "openai"},
		{Provider: "custom", APIKey: "k", Model: "m"},
		{Provider: "custom", APIKey: "k", BaseURL: "http://x"},
		{Provider: "openai", APIKey: "k", JSONMode: "xml"},
	} {
		if _, _, err := New(s); err == nil {
			t.Errorf("settings %+v were accepted", s)
		}
	}
	var req map[string]any
	ts := fakeChat(t, "Sorry, I cannot help with that.", &req)
	p, _, _ := New(Settings{Provider: "custom", APIKey: "secret", BaseURL: ts.URL, Model: "m"})
	if _, err := p.Parse(context.Background(), testCatalog, "приход"); err == nil {
		t.Error("an answer without JSON was accepted")
	}
}
