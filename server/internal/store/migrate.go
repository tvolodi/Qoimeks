package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
)

// publicOnly lists migrations that run only in the default schema (the tenant
// registry and the like). Every other migration runs in the default schema AND in
// every tenant schema, so a new migration reaches existing tenants on the next start.
// Rule for new migrations: they run in every tenant schema unless listed here.
var publicOnly = map[string]bool{
	"005_tenants.sql":                  true,
	"006_tenants_drop_access_code.sql": true,
}

// Migrate applies the embedded migrations that have not run yet to the default schema.
func (s *Store) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	return s.migrate(ctx, sub, false)
}

// migrate applies the *.sql files of fsys in name order to the schema this store's
// connections point at. Concurrent callers (two server instances, or a repeated
// tenant request) are serialized per schema by an advisory lock, and each file is
// re-checked after the lock is taken, so every file runs once.
func (s *Store) migrate(ctx context.Context, fsys fs.FS, tenant bool) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockSchemaMigrations(ctx, tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`create table if not exists schema_migrations (name text primary key, applied_at timestamptz not null default now())`)
		return err
	})
	if err != nil {
		return err
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if tenant && publicOnly[name] {
			continue
		}
		sql, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			if err := lockSchemaMigrations(ctx, tx); err != nil {
				return err
			}
			var done bool
			if err := tx.QueryRow(ctx, `select exists(select 1 from schema_migrations where name = $1)`, name).Scan(&done); err != nil {
				return err
			}
			if done {
				return nil
			}
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `insert into schema_migrations (name) values ($1)`, name)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func lockSchemaMigrations(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('qoimeks-migrate:' || coalesce(current_schema(), '')))`)
	return err
}
