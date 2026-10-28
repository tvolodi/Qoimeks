// Package providers holds the language-model and speech provider settings: the
// stored (encrypted) values written by the admin and the per-request Resolver that
// combines them with the environment.
package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/secrets"
	"qoimeks/server/internal/store"
)

const (
	KindLLM = "llm"
	KindSTT = "stt"

	STTElevenLabs = "elevenlabs"
	STTOpenAI     = "openai-compatible"

	maxModelLen = 200
	maxKeyLen   = 512
	hintLen     = 4
	minHintKey  = 12 // shorter keys get no hint: four characters would reveal too much
)

// ErrNoSettingsKey: an API key cannot be stored because SETTINGS_KEY is missing or invalid.
var ErrNoSettingsKey = errors.New("SETTINGS_KEY is not set: provider keys cannot be stored")

func aad(kind string) string { return "qoimeks/provider_settings/v1:" + kind }

// Service writes provider settings and shows them in masked form. Store must be the
// default-schema store: the writer refuses a tenant store.
type Service struct {
	Store *store.Store
	Key   secrets.Key
}

// ProviderSettingInput is a save request. An empty APIKey with KeepKey false clears the stored key.
type ProviderSettingInput struct {
	Kind, Provider, Model, APIKey string
	KeepKey                       bool
}

// ProviderSettingView is the only shape in which settings leave the server: it has
// no field that can hold a key.
type ProviderSettingView struct {
	Kind      string    `json:"kind"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	HasKey    bool      `json:"has_key"`
	KeyMask   string    `json:"key_mask"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Service) Save(ctx context.Context, in ProviderSettingInput) error {
	switch in.Kind {
	case KindLLM:
		if _, ok := parse.Providers[in.Provider]; !ok {
			return invalid("unknown LLM provider %q (known: %s)", in.Provider, parse.ProviderNames())
		}
	case KindSTT:
		if in.Provider != STTElevenLabs && in.Provider != STTOpenAI {
			return invalid("unknown speech provider %q", in.Provider)
		}
	default:
		return invalid("unknown kind %q", in.Kind)
	}
	if len(in.Model) > maxModelLen {
		return invalid("model name is longer than %d characters", maxModelLen)
	}
	key := strings.TrimSpace(in.APIKey)
	if len(key) > maxKeyLen || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return invalid("the API key is too long or has control characters")
	}
	row := store.ProviderSettingRow{Kind: in.Kind, Provider: in.Provider, Model: strings.TrimSpace(in.Model), KeepKey: in.KeepKey && key == ""}
	if key != "" {
		if !s.Key.Valid() {
			return ErrNoSettingsKey
		}
		blob, err := s.Key.Seal(aad(in.Kind), []byte(key))
		if err != nil {
			return fmt.Errorf("encrypt provider key: %w", err)
		}
		row.KeyEnc = blob
		if len(key) >= minHintKey {
			row.KeyHint = key[len(key)-hintLen:]
		}
	}
	return s.Store.UpsertProviderSetting(ctx, row)
}

// View lists the stored settings, masked.
func (s *Service) View(ctx context.Context) ([]ProviderSettingView, error) {
	rows, err := s.Store.ListProviderSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProviderSettingView, 0, len(rows))
	for _, r := range rows {
		v := ProviderSettingView{Kind: r.Kind, Provider: r.Provider, Model: r.Model, UpdatedAt: r.UpdatedAt}
		if len(r.KeyEnc) > 0 {
			v.HasKey = true
			v.KeyMask = "••••" + r.KeyHint
		}
		out = append(out, v)
	}
	return out, nil
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", store.ErrInvalid, fmt.Sprintf(format, a...))
}
