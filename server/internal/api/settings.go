package api

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/providers"
	"qoimeks/server/internal/store"
)

const (
	maxSettingsBody = 16 << 10
	testTimeout     = 30 * time.Second
	maxTestError    = 300
)

// SettingsService saves provider settings and shows them masked (providers.Service).
type SettingsService interface {
	Save(ctx context.Context, in providers.ProviderSettingInput) error
	View(ctx context.Context) ([]providers.ProviderSettingView, error)
}

func (s *Server) settingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/providers", s.guardSettings(s.getProviderSettings))
	mux.HandleFunc("PUT /api/settings/providers", s.guardSettings(s.putProviderSettings))
	mux.HandleFunc("POST /api/settings/providers/test", s.guardSettings(s.testProvider))
}

// guardSettings lets only the shared ACCESS_TOKEN through. requireToken has already
// answered 401 for a missing or wrong code; a tenant's code arrives here with its
// schema in the context and is refused. With no ACCESS_TOKEN the page is refused too:
// settings must never be open to whoever can reach the server.
func (s *Server) guardSettings(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.AccessToken == "" {
			writeError(w, http.StatusForbidden, "настройки доступны только при заданном ACCESS_TOKEN")
			return
		}
		if _, tenant := r.Context().Value(schemaKey{}).(string); tenant {
			writeError(w, http.StatusForbidden, "настройки доступны только общему коду доступа")
			return
		}
		if s.Settings == nil {
			writeError(w, http.StatusServiceUnavailable, "настройки не подключены")
			return
		}
		next(w, r)
	}
}

type providerSettingsAnswer struct {
	Settings     []providers.ProviderSettingView `json:"settings"`
	LLMProviders []string                        `json:"llm_providers"`
	STTProviders []string                        `json:"stt_providers"`
}

func (s *Server) getProviderSettings(w http.ResponseWriter, r *http.Request) {
	views, err := s.Settings.View(r.Context())
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, providerSettingsAnswer{
		Settings:     views,
		LLMProviders: strings.Split(parse.ProviderNames(), ", "),
		STTProviders: []string{providers.STTElevenLabs, providers.STTOpenAI},
	})
}

type providerSettingRequest struct {
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
	KeepKey  bool   `json:"keep_key"`
}

func (s *Server) putProviderSettings(w http.ResponseWriter, r *http.Request) {
	var in providerSettingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingsBody)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	err := s.Settings.Save(r.Context(), providers.ProviderSettingInput{
		Kind: in.Kind, Provider: in.Provider, Model: in.Model, APIKey: in.APIKey, KeepKey: in.KeepKey,
	})
	if errors.Is(err, providers.ErrNoSettingsKey) {
		writeError(w, http.StatusConflict, "на сервере не задан SETTINGS_KEY: ключ нельзя сохранить. Попросите администратора сервера задать SETTINGS_KEY")
		return
	}
	if err != nil {
		storeError(w, r, err)
		return
	}
	s.getProviderSettings(w, r)
}

type testRequest struct {
	Kind string `json:"kind"`
}

type testAnswer struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// testProvider makes one cheap call with the saved settings (or the environment) and
// reports ok or the error, with every known key removed from the text.
func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	var in testRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingsBody)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "request body is not valid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), testTimeout)
	defer cancel()
	var err error
	switch in.Kind {
	case providers.KindLLM:
		err = s.testLLM(ctx)
	case providers.KindSTT:
		err = s.testSTT(ctx)
	default:
		writeError(w, http.StatusBadRequest, "unknown kind")
		return
	}
	if err == nil {
		slog.Info("provider test", "kind", in.Kind, "ok", true)
		writeJSON(w, http.StatusOK, testAnswer{OK: true})
		return
	}
	msg := s.newRedactor(r).str(err.Error())
	if len(msg) > maxTestError {
		msg = msg[:maxTestError] + "…"
	}
	slog.Info("provider test", "kind", in.Kind, "ok", false, "err", msg)
	writeJSON(w, http.StatusOK, testAnswer{OK: false, Message: msg})
}

func (s *Server) testLLM(ctx context.Context) error {
	p, _, err := s.Providers.LLM(ctx)
	if err != nil {
		return err
	}
	_, err = p.Parse(ctx, store.Catalog{}, "приход 1 шт")
	if errors.Is(err, parse.ErrRefused) {
		return nil // the service answered
	}
	return err
}

func (s *Server) testSTT(ctx context.Context) error {
	t, _, err := s.Providers.Transcriber(ctx)
	if err != nil {
		return err
	}
	_, err = t.Transcribe(ctx, silentWAV(), "test.wav", "ru")
	return err
}

// silentWAV is 0.1 s of 8 kHz 16-bit mono silence: the smallest sound a service accepts.
func silentWAV() []byte {
	const data = 1600
	b := make([]byte, 44+data)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], 36+data)
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 8000)
	binary.LittleEndian.PutUint32(b[28:], 16000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], data)
	return b
}
