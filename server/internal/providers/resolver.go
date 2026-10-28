package providers

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/secrets"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/stt"
)

// ErrNotConfigured: neither the stored settings nor the environment name a usable provider and key.
var ErrNotConfigured = errors.New("provider is not configured")

// Source says where an effective config came from.
type Source string

const (
	SourceStored      Source = "stored"
	SourceEnv         Source = "env"
	SourceEnvFallback Source = "env-fallback" // a stored row exists but its key cannot be used
)

// STTConfig is the effective speech configuration.
type STTConfig struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
}

var elevenVars = []string{"ELEVENLABS_API_KEY", "ELEVENLAB_API_KEY", "elevenlabs_api_key", "elevenlab_api_key"}

// llmAutoOrder is the order in which a provider is picked when LLM_PROVIDER is not set.
var llmAutoOrder = []string{"anthropic", "openai", "xai", "deepseek", "minimax"}

// Resolver gives the effective provider config per request: the stored value first,
// the environment as the fallback. It reads the database on every call, so a change
// is seen by the next request without a restart. Store is the default-schema store.
type Resolver struct {
	Store  *store.Store
	Key    secrets.Key
	Env    func(string) string // os.Getenv when nil
	Logger *slog.Logger        // slog.Default() when nil

	mu     sync.Mutex
	warned map[string]bool
}

func (r *Resolver) env(k string) string {
	if r.Env != nil {
		return r.Env(k)
	}
	return os.Getenv(k)
}

func (r *Resolver) first(keys ...string) string {
	for _, k := range keys {
		if v := r.env(k); v != "" {
			return v
		}
	}
	return ""
}

func (r *Resolver) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// stored reads the row of kind and, when it has a key, decrypts it. usable is false
// when the whole kind must come from the environment (no row, read error, key unusable).
func (r *Resolver) stored(ctx context.Context, kind string) (row store.ProviderSettingRow, key string, usable bool, src Source) {
	row, err := r.Store.GetProviderSetting(ctx, kind)
	if errors.Is(err, store.ErrNotFound) {
		return row, "", false, SourceEnv
	}
	if err != nil {
		r.log().Error("reading provider settings failed, using environment", "kind", kind, "err", err)
		return row, "", false, SourceEnv
	}
	if len(row.KeyEnc) == 0 {
		return row, "", true, SourceStored
	}
	pt, err := r.Key.Open(aad(kind), row.KeyEnc)
	if err != nil {
		reason := "cannot decrypt"
		if !r.Key.Valid() {
			reason = "no SETTINGS_KEY"
		}
		r.warnOnce(kind, row.UpdatedAt, reason)
		return row, "", false, SourceEnvFallback
	}
	return row, string(pt), true, SourceStored
}

func (r *Resolver) warnOnce(kind string, updated time.Time, reason string) {
	id := kind + "/" + updated.UTC().Format(time.RFC3339Nano) + "/" + reason
	r.mu.Lock()
	if r.warned == nil {
		r.warned = map[string]bool{}
	}
	done := r.warned[id]
	r.warned[id] = true
	r.mu.Unlock()
	if !done {
		r.log().Warn("stored provider key unusable, using environment", "kind", kind, "reason", reason)
	}
}

func (r *Resolver) envLLMProvider() string {
	if p := r.env("LLM_PROVIDER"); p != "" {
		return p
	}
	for _, name := range llmAutoOrder {
		if r.env(parse.Providers[name].KeyVar) != "" {
			return name
		}
	}
	return ""
}

func (r *Resolver) envLLM() (parse.Settings, error) {
	p := r.envLLMProvider()
	if p == "" {
		return parse.Settings{}, ErrNotConfigured
	}
	return parse.Settings{
		Provider: p,
		APIKey:   r.first("LLM_API_KEY", parse.Providers[strings.ToLower(p)].KeyVar),
		BaseURL:  r.env("LLM_BASE_URL"),
		Model:    r.env("LLM_MODEL"),
		JSONMode: r.env("LLM_JSON_MODE"),
	}, nil
}

// LLMConfig is the effective language-model config. A stored provider that is not the
// environment's (or "custom") does not inherit the environment's base URL, JSON mode or model.
func (r *Resolver) LLMConfig(ctx context.Context) (parse.Settings, Source, error) {
	row, key, usable, src := r.stored(ctx, KindLLM)
	if !usable {
		s, err := r.envLLM()
		return s, src, err
	}
	s := parse.Settings{Provider: row.Provider, APIKey: key, Model: row.Model}
	if strings.EqualFold(row.Provider, r.envLLMProvider()) || row.Provider == "custom" {
		s.BaseURL, s.JSONMode = r.env("LLM_BASE_URL"), r.env("LLM_JSON_MODE")
		if s.Model == "" {
			s.Model = r.env("LLM_MODEL")
		}
	}
	if s.APIKey == "" {
		s.APIKey = r.first("LLM_API_KEY", parse.Providers[row.Provider].KeyVar)
	}
	return s, SourceStored, nil
}

// LLM builds the parser for this request and its label. ErrNotConfigured: none.
func (r *Resolver) LLM(ctx context.Context) (parse.Parser, string, error) {
	s, _, err := r.LLMConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	return parse.New(s)
}

func (r *Resolver) envSTT() (STTConfig, error) {
	key, url := r.env("STT_API_KEY"), r.env("STT_BASE_URL")
	if key != "" || url != "" {
		return r.sttConfig(STTOpenAI, key, r.env("STT_MODEL")), nil
	}
	if eleven := r.first(elevenVars...); eleven != "" {
		return r.sttConfig(STTElevenLabs, eleven, r.env("STT_MODEL")), nil
	}
	return STTConfig{}, ErrNotConfigured
}

func (r *Resolver) sttConfig(provider, key, model string) STTConfig {
	c := STTConfig{Provider: provider, APIKey: key, Model: model}
	if provider == STTOpenAI {
		c.BaseURL = r.env("STT_BASE_URL")
		if c.BaseURL == "" {
			c.BaseURL = "https://api.openai.com/v1"
		}
		if c.Model == "" {
			c.Model = "gpt-4o-transcribe"
		}
	} else if c.Model == "" {
		c.Model = "scribe_v2"
	}
	return c
}

// STTConfig is the effective speech config.
func (r *Resolver) STTConfig(ctx context.Context) (STTConfig, Source, error) {
	row, key, usable, src := r.stored(ctx, KindSTT)
	if !usable {
		c, err := r.envSTT()
		return c, src, err
	}
	if key == "" {
		if row.Provider == STTElevenLabs {
			key = r.first(elevenVars...)
		} else {
			key = r.env("STT_API_KEY")
		}
	}
	if key == "" && !(row.Provider == STTOpenAI && r.env("STT_BASE_URL") != "") {
		return STTConfig{}, SourceStored, ErrNotConfigured
	}
	return r.sttConfig(row.Provider, key, row.Model), SourceStored, nil
}

// Transcriber builds the speech client for this request and its name. ErrNotConfigured: none.
func (r *Resolver) Transcriber(ctx context.Context) (stt.Transcriber, string, error) {
	c, _, err := r.STTConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	if c.Provider == STTElevenLabs {
		return stt.NewElevenLabs(c.APIKey, c.Model), c.Provider, nil
	}
	return stt.NewOpenAICompatible(c.BaseURL, c.APIKey, c.Model), c.Provider, nil
}

// Secrets returns the stored keys that can be decrypted, for redaction of recorded errors.
func (r *Resolver) Secrets(ctx context.Context) []string {
	var out []string
	for _, kind := range []string{KindLLM, KindSTT} {
		row, err := r.Store.GetProviderSetting(ctx, kind)
		if err != nil || len(row.KeyEnc) == 0 {
			continue
		}
		if pt, err := r.Key.Open(aad(kind), row.KeyEnc); err == nil {
			out = append(out, string(pt))
		}
	}
	return out
}
