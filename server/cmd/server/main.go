package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"qoimeks/server/internal/api"
	"qoimeks/server/internal/config"
	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/stt"
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
	if os.Getenv("SEED_DEMO") == "1" {
		if err := st.SeedDemo(ctx); err != nil {
			slog.Error("demo seed failed", "err", err)
			os.Exit(1)
		}
	}

	srv := &api.Server{Store: st}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		srv.Parser = parse.NewClaude(config.Get("claude-opus-5-5", "LLM_MODEL"))
	} else {
		slog.Warn("ANTHROPIC_API_KEY is not set: voice and phrase parsing are disabled")
	}
	eleven := config.Get("", "ELEVENLABS_API_KEY", "ELEVENLAB_API_KEY", "elevenlabs_api_key", "elevenlab_api_key")
	sttKey, sttURL := os.Getenv("STT_API_KEY"), os.Getenv("STT_BASE_URL")
	switch {
	case sttKey != "" || sttURL != "":
		srv.Transcriber = stt.NewOpenAICompatible(
			config.Get("https://api.openai.com/v1", "STT_BASE_URL"), sttKey, config.Get("gpt-4o-transcribe", "STT_MODEL"))
	case eleven != "":
		srv.Transcriber = stt.NewElevenLabs(eleven, config.Get("scribe_v2", "STT_MODEL"))
	default:
		slog.Warn("no ELEVENLABS_API_KEY or STT_API_KEY: voice input is disabled")
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
