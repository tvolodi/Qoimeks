package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/api"
	"qoimeks/server/internal/config"
	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/providers"
	"qoimeks/server/internal/secrets"
	"qoimeks/server/internal/store"
)

func main() {
	config.LoadDotEnv(".env", "../.env")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	st, err := store.Open(ctx, config.Get(config.DefaultDatabaseURL, "DATABASE_URL"))
	if err != nil {
		slog.Error("cannot connect to the database", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}
	if err := st.MigrateTenants(ctx); err != nil {
		// Other tenants and the default schema still work; the failing tenant's requests error.
		slog.Error("some tenant schemas could not be migrated", "err", err)
	}
	if os.Getenv("SEED_DEMO") == "1" {
		if err := st.SeedDemo(ctx); err != nil {
			slog.Error("demo seed failed", "err", err)
			os.Exit(1)
		}
	}

	srv := &api.Server{
		Store:             st,
		AccessToken:       os.Getenv("ACCESS_TOKEN"),
		ProvisioningToken: os.Getenv("PROVISIONING_TOKEN"),
		PublicURL:         os.Getenv("PUBLIC_URL"),
	}
	defer srv.Close()
	if v := os.Getenv("ACCESS_TOKEN_TENANT"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			slog.Error("ACCESS_TOKEN_TENANT is not a tenant UUID", "err", err)
			os.Exit(1)
		}
		srv.AccessTokenTenant = id
		if err := srv.CheckAccessTokenTenant(ctx); err != nil {
			slog.Error("cannot start", "err", err)
			os.Exit(1)
		}
		slog.Info("ACCESS_TOKEN serves a tenant schema", "tenant", id)
	}
	switch {
	case srv.ProvisioningToken == "":
		slog.Info("PROVISIONING_TOKEN is not set: the tenant API (/admin/v1) is off")
	case len(srv.ProvisioningToken) < 32:
		slog.Warn("PROVISIONING_TOKEN is shorter than 32 characters: use a long random value")
	}
	if srv.AccessToken == "" {
		slog.Warn("ACCESS_TOKEN is not set: anyone who can reach this server can use it")
	}
	// Provider settings: stored (encrypted with SETTINGS_KEY) first, environment second.
	var settingsKey secrets.Key
	switch raw := os.Getenv("SETTINGS_KEY"); {
	case raw == "":
		slog.Info("SETTINGS_KEY is not set: stored provider keys are disabled, provider keys come from the environment")
	default:
		if settingsKey, err = secrets.ParseKey(raw); err != nil {
			slog.Error("SETTINGS_KEY is invalid: expected base64 of 32 bytes; stored provider keys are disabled")
		}
	}
	resolver := &providers.Resolver{Store: st, Key: settingsKey}
	srv.Providers = resolver
	cfg, source, err := resolver.LLMConfig(ctx)
	switch {
	case errors.Is(err, providers.ErrNotConfigured):
		slog.Warn("no LLM provider configured: spoken phrases are shown but not turned into operations")
	default:
		_, label, err := parse.New(cfg)
		if err != nil {
			if source == providers.SourceStored {
				slog.Error("stored LLM configuration is unusable", "err", err)
			} else {
				slog.Error("LLM configuration", "err", err)
				os.Exit(1)
			}
		} else {
			slog.Info("phrases are parsed by", "llm", label, "source", source)
		}
	}
	if stc, source, err := resolver.STTConfig(ctx); err != nil {
		slog.Warn("no ELEVENLABS_API_KEY or STT_API_KEY: voice input is disabled")
	} else {
		slog.Info("speech is recognised by", "stt", stc.Provider, "source", source)
	}

	// Every key the server may hold, set or not; short and empty values are ignored later.
	for _, name := range []string{"LLM_API_KEY", "STT_API_KEY", "ELEVENLABS_API_KEY", "ELEVENLAB_API_KEY",
		"elevenlabs_api_key", "elevenlab_api_key"} {
		srv.ProviderKeys = append(srv.ProviderKeys, os.Getenv(name))
	}
	for _, p := range parse.Providers {
		srv.ProviderKeys = append(srv.ProviderKeys, os.Getenv(p.KeyVar))
	}

	httpServer := &http.Server{
		Addr:              config.Get(":8080", "LISTEN_ADDR"),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()
	slog.Info("listening", "addr", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
