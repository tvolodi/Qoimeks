package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/secrets"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func key(t *testing.T, fill byte) secrets.Key {
	t.Helper()
	k, err := secrets.ParseKey(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func setup(t *testing.T, env map[string]string) (*Service, *Resolver, *bytes.Buffer) {
	t.Helper()
	st := storetest.New(t)
	logs := &bytes.Buffer{}
	k := key(t, 1)
	return &Service{Store: st, Key: k},
		&Resolver{Store: st, Key: k, Env: envOf(env), Logger: slog.New(slog.NewTextHandler(logs, nil))}, logs
}

func TestKeyRoundTripAndMask(t *testing.T) {
	const plain = "sk-test-abcdefghijklmnopqrstuvwxyz"
	svc, res, _ := setup(t, nil)
	ctx := context.Background()
	if err := svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", Model: "m1", APIKey: " " + plain + " "}); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Store.GetProviderSetting(ctx, KindLLM)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(row.KeyEnc, []byte(plain)) || strings.Contains(fmt.Sprintf("%+v", row), plain) {
		t.Error("stored row holds the plaintext key")
	}
	cfg, src, err := res.LLMConfig(ctx)
	if err != nil || cfg.APIKey != plain || cfg.Provider != "openai" || cfg.Model != "m1" || src != SourceStored {
		t.Fatalf("cfg = %+v %v %v", cfg, src, err)
	}
	views, err := svc.View(ctx)
	if err != nil || len(views) != 1 {
		t.Fatalf("views = %v %v", views, err)
	}
	if views[0].KeyMask != "••••wxyz" || !views[0].HasKey {
		t.Errorf("view = %+v", views[0])
	}
	js, _ := json.Marshal(views)
	if strings.Contains(string(js), plain) {
		t.Error("view JSON holds the key")
	}
}

func TestUndecryptableKeyFallsBackToEnv(t *testing.T) {
	const stored, envKey = "stored-secret-value-1234", "env-secret-value-5678"
	for name, mod := range map[string]func(*Resolver, *testing.T){
		"changed key": func(r *Resolver, t *testing.T) { r.Key = key(t, 2) },
		"missing key": func(r *Resolver, t *testing.T) { r.Key = secrets.Key{} },
	} {
		t.Run(name, func(t *testing.T) {
			svc, res, logs := setup(t, map[string]string{"OPENAI_API_KEY": envKey})
			ctx := context.Background()
			if err := svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "xai", Model: "m", APIKey: stored}); err != nil {
				t.Fatal(err)
			}
			mod(res, t)
			for i := 0; i < 3; i++ {
				cfg, src, err := res.LLMConfig(ctx)
				if err != nil || cfg.APIKey != envKey || cfg.Provider != "openai" || src != SourceEnvFallback {
					t.Fatalf("cfg = %+v %v %v", cfg, src, err)
				}
			}
			row, _ := svc.Store.GetProviderSetting(ctx, KindLLM)
			out := logs.String()
			for _, bad := range []string{stored, envKey, row.KeyHint, base64.StdEncoding.EncodeToString(row.KeyEnc)} {
				if strings.Contains(out, bad) {
					t.Errorf("log contains %q: %s", bad, out)
				}
			}
			if n := strings.Count(out, "stored provider key unusable"); n != 1 {
				t.Errorf("warnings = %d, want 1", n)
			}
			if got := res.Secrets(ctx); len(got) != 0 {
				t.Errorf("Secrets = %v", got)
			}
		})
	}
}

func TestSettingsChangeSeenWithoutRestart(t *testing.T) {
	svc, res, _ := setup(t, map[string]string{"OPENAI_API_KEY": "env-openai-key-0000"})
	ctx := context.Background()
	_, label, err := res.LLM(ctx)
	if err != nil || label != "openai gpt-4o-mini" {
		t.Fatalf("env label = %q %v", label, err)
	}
	for _, c := range []struct{ provider, model, want string }{
		{"openai", "m1", "openai m1"},
		{"deepseek", "m2", "deepseek m2"},
	} {
		if err := svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: c.provider, Model: c.model, APIKey: "stored-key-" + c.provider}); err != nil {
			t.Fatal(err)
		}
		if _, label, err := res.LLM(ctx); err != nil || label != c.want {
			t.Errorf("label = %q %v, want %q", label, err, c.want)
		}
	}
	if _, _, err := res.Transcriber(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("stt before save: %v", err)
	}
	for _, c := range []struct{ provider, model string }{{STTElevenLabs, "scribe_v2"}, {STTOpenAI, "whisper-1"}} {
		if err := svc.Save(ctx, ProviderSettingInput{Kind: KindSTT, Provider: c.provider, Model: c.model, APIKey: "stt-key-" + c.provider}); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := res.STTConfig(ctx)
		if err != nil || cfg.Provider != c.provider || cfg.Model != c.model {
			t.Errorf("stt cfg = %+v %v", cfg, err)
		}
		if _, name, err := res.Transcriber(ctx); err != nil || name != c.provider {
			t.Errorf("transcriber = %q %v", name, err)
		}
	}
}

func TestStoredProviderIgnoresForeignBaseURL(t *testing.T) {
	svc, res, _ := setup(t, map[string]string{
		"LLM_PROVIDER": "custom", "LLM_BASE_URL": "https://llm.internal/v1", "LLM_MODEL": "env-model", "STT_BASE_URL": "https://stt.internal/v1",
	})
	ctx := context.Background()
	svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", APIKey: "stored-key-openai"})
	if cfg, _, _ := res.LLMConfig(ctx); cfg.BaseURL != "" || cfg.Model != "" {
		t.Errorf("openai inherited env settings: %+v", cfg)
	}
	svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "custom", APIKey: "stored-key-custom"})
	if cfg, _, _ := res.LLMConfig(ctx); cfg.BaseURL != "https://llm.internal/v1" || cfg.Model != "env-model" {
		t.Errorf("custom lost env settings: %+v", cfg)
	}
	svc.Save(ctx, ProviderSettingInput{Kind: KindSTT, Provider: STTElevenLabs, APIKey: "stt-key-eleven"})
	if cfg, _, _ := res.STTConfig(ctx); cfg.BaseURL != "" || cfg.Model != "scribe_v2" {
		t.Errorf("elevenlabs cfg = %+v", cfg)
	}
	svc.Save(ctx, ProviderSettingInput{Kind: KindSTT, Provider: STTOpenAI, APIKey: "stt-key-openai"})
	if cfg, _, _ := res.STTConfig(ctx); cfg.BaseURL != "https://stt.internal/v1" || cfg.Model != "gpt-4o-transcribe" {
		t.Errorf("openai-compatible cfg = %+v", cfg)
	}
}

func TestEnvOnlyWhenNothingStored(t *testing.T) {
	_, res, _ := setup(t, map[string]string{"ELEVENLABS_API_KEY": "eleven-env-key", "ANTHROPIC_API_KEY": "anthropic-env-key"})
	ctx := context.Background()
	if cfg, src, err := res.LLMConfig(ctx); err != nil || cfg.Provider != "anthropic" || src != SourceEnv {
		t.Errorf("llm = %+v %v %v", cfg, src, err)
	}
	if cfg, src, err := res.STTConfig(ctx); err != nil || cfg.Provider != STTElevenLabs || cfg.Model != "scribe_v2" || src != SourceEnv {
		t.Errorf("stt = %+v %v %v", cfg, src, err)
	}
	_, empty, _ := setup(t, nil)
	if _, _, err := empty.LLMConfig(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v", err)
	}
}

func TestSaveRules(t *testing.T) {
	svc, _, _ := setup(t, nil)
	ctx := context.Background()
	noKey := &Service{Store: svc.Store}
	if err := noKey.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", APIKey: "some-key-123456"}); !errors.Is(err, ErrNoSettingsKey) {
		t.Errorf("err = %v", err)
	}
	if rows, _ := svc.Store.ListProviderSettings(ctx); len(rows) != 0 {
		t.Errorf("rows stored without SETTINGS_KEY: %v", rows)
	}
	if err := noKey.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", Model: "m"}); err != nil {
		t.Errorf("model-only save: %v", err)
	}
	for name, in := range map[string]ProviderSettingInput{
		"kind":     {Kind: "x", Provider: "openai"},
		"llm prov": {Kind: KindLLM, Provider: "nope"},
		"stt prov": {Kind: KindSTT, Provider: "openai"},
		"model":    {Kind: KindLLM, Provider: "openai", Model: strings.Repeat("m", 201)},
		"ctrl key": {Kind: KindLLM, Provider: "openai", APIKey: "ab\ncd"},
		"long key": {Kind: KindLLM, Provider: "openai", APIKey: strings.Repeat("k", 513)},
	} {
		if err := svc.Save(ctx, in); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// KeepKey leaves the stored key usable after a model change.
	svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", APIKey: "keep-this-key-9999"})
	svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", Model: "m3", KeepKey: true})
	res := &Resolver{Store: svc.Store, Key: svc.Key, Env: envOf(nil)}
	if cfg, _, _ := res.LLMConfig(ctx); cfg.APIKey != "keep-this-key-9999" || cfg.Model != "m3" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestWriterRefusesTenantSchema(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	tn, _, _, err := st.CreateTenant(ctx, store.TenantInput{ID: uuid.NewString(), Name: "Склад", Language: "ru", Tier: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := st.TenantStore(ctx, tn.SchemaName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	svc := &Service{Store: ts, Key: key(t, 1)}
	err = svc.Save(ctx, ProviderSettingInput{Kind: KindLLM, Provider: "openai", APIKey: "some-key-1234567"})
	if !errors.Is(err, store.ErrTenantSchema) {
		t.Errorf("err = %v", err)
	}
	if rows, err := st.ListProviderSettings(ctx); err != nil || len(rows) != 0 {
		t.Errorf("default rows = %v %v", rows, err)
	}
}
