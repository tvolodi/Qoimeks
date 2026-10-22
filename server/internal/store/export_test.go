package store

import (
	"context"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Test-only access for the external store_test package.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) MigrateFS(ctx context.Context, fsys fs.FS, tenant bool) error {
	return s.migrate(ctx, fsys, tenant)
}

func SetPublicOnly(name string) (restore func()) {
	publicOnly[name] = true
	return func() { delete(publicOnly, name) }
}
