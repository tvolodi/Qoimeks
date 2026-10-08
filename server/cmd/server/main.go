package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"qoimeks/server/internal/api"
	"qoimeks/server/internal/parse"
	"qoimeks/server/internal/store"
	"qoimeks/server/internal/stt"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	st, err := store.Open(ctx, env("DATABASE_URL", "postgres://qoimeks:qoimeks@127.0.0.1:5433/qoimeks"))
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
		srv.Parser = parse.NewClaude(env("LLM_MODEL", "claude-opus-5-5"))
	} else {
		slog.Warn("ANTHROPIC_API_KEY is not set: voice and phrase parsing are disabled")
	}
	if key := os.Getenv("STT_API_KEY"); key != "" || os.Getenv("STT_BASE_URL") != "" {
		srv.Transcriber = stt.NewOpenAICompatible(
			env("STT_BASE_URL", "https://api.openai.com/v1"), key, env("STT_MODEL", "gpt-4o-transcribe"))
	} else {
		slog.Warn("STT_API_KEY is not set: voice input is disabled")
	}

	httpServer := &http.Server{
		Addr:              env("LISTEN_ADDR", ":8080"),
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
