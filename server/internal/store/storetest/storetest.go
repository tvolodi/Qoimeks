// Package storetest gives tests a private, freshly migrated database.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"qoimeks/server/internal/store"
)

// New creates a throwaway database with the demo catalog and drops it when the
// test ends. Tests are skipped unless TEST_DATABASE_URL points at a PostgreSQL
// server where the user may create databases.
func New(t *testing.T) *store.Store {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	name := fmt.Sprintf("qoimeks_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	st, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		st.Close()
		admin.Exec(ctx, "drop database "+name+" with (force)")
		admin.Close(ctx)
	})
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := st.SeedDemo(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return st
}
