// Package storetest gives tests a private, freshly migrated database.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"qoimeks/server/internal/store"
)

var urls sync.Map // *store.Store -> database URL

// Exec runs sql on the database of a store made by New, for tests that need the
// database to misbehave (a trigger that fails, for example).
func Exec(t *testing.T, st *store.Store, sql string) {
	t.Helper()
	u, ok := urls.Load(st)
	if !ok {
		t.Fatal("storetest.Exec: store was not made by storetest.New")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, u.(string))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

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
	urls.Store(st, u.String())
	t.Cleanup(func() {
		urls.Delete(st)
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
