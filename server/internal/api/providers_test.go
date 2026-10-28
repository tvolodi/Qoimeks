package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/stt"
)

const storedSecret = "stored-secret-key-1"

type namedParser struct{ name string }

func (p namedParser) Parse(context.Context, store.Catalog, string) (parse.Draft, error) {
	return parse.Draft{}, errors.New("model " + p.name + " refused, key " + storedSecret)
}

// swapResolver changes its answer between calls, as a saved setting does.
type swapResolver struct{ name string }

func (r *swapResolver) LLM(context.Context) (parse.Parser, string, error) {
	return namedParser{r.name}, r.name, nil
}

func (r *swapResolver) Transcriber(context.Context) (stt.Transcriber, string, error) {
	return nil, "", errors.New("not configured")
}

func (r *swapResolver) Secrets(context.Context) []string { return []string{storedSecret} }

func TestServerAsksResolverPerRequest(t *testing.T) {
	res := &swapResolver{name: "first"}
	c, _, _ := setup(t, &Server{Providers: res})
	c.do("POST", "/api/parse", map[string]string{"text": "приход"}, 502, nil)
	res.name = "second"
	c.do("POST", "/api/parse", map[string]string{"text": "расход"}, 502, nil)
	c.do("POST", "/api/voice", nil, 503, nil) // speech not available: the form still works

	rows, err := c.store.RecentFeedback(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range rows {
		if f.Kind != "llm_error" || f.Provider == nil {
			continue
		}
		seen[*f.Provider] = true
		if strings.Contains(f.Text, storedSecret) {
			t.Errorf("stored key not redacted: %q", f.Text)
		}
	}
	if !seen["first"] || !seen["second"] {
		t.Errorf("recorded providers = %v, want first and second", seen)
	}
}
