// Command migrate-tenant copies the ledger of the default schema into a tenant's schema.
//
//	go run ./cmd/migrate-tenant --tenant-id <uuid> [--source-schema public] [--dry-run]
//
// It copies, never moves: the source is left as it is. See docs/migrate-to-tenant.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"qoimeks/server/internal/config"
	"qoimeks/server/internal/store"
)

func main() {
	tenant := flag.String("tenant-id", "", "id of the existing tenant to copy into")
	source := flag.String("source-schema", "public", "schema to copy from")
	dryRun := flag.Bool("dry-run", false, "do everything, report, then roll back")
	flag.Parse()
	id, err := uuid.Parse(*tenant)
	if err != nil {
		fmt.Fprintln(os.Stderr, "--tenant-id must be a tenant UUID")
		os.Exit(2)
	}
	config.LoadDotEnv(".env", "../.env")

	ctx := context.Background()
	st, err := store.Open(ctx, config.Get(config.DefaultDatabaseURL, "DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot connect to the database") // the error text may hold the URL
		os.Exit(1)
	}
	defer st.Close()

	rep, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: id, SourceSchema: *source, DryRun: *dryRun})
	printReport(rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAILED, nothing was changed:", err)
		if errors.Is(err, store.ErrTargetNotEmpty) {
			fmt.Fprintln(os.Stderr, "the tenant already has data; this tool only fills an empty tenant")
		}
		os.Exit(1)
	}
	if rep.DryRun {
		fmt.Println("DRY RUN: all checks passed, rolled back, nothing was changed")
	} else {
		fmt.Println("DONE: copied and verified, committed")
	}
}

func printReport(r store.CopyReport) {
	if r.TargetSchema == "" {
		return
	}
	fmt.Printf("tenant %s: %s -> %s\n", r.TenantID, r.SourceSchema, r.TargetSchema)
	for _, t := range r.Tables {
		state := "ok"
		if t.SourceRows != t.TargetRows || t.SourceSum != t.TargetSum {
			state = "MISMATCH"
		}
		fmt.Printf("  %-10s rows %d -> %d  md5 %s -> %s  %s  %v\n", t.Name, t.SourceRows, t.TargetRows, t.SourceSum, t.TargetSum, state, t.Elapsed.Round(time.Millisecond))
	}
	if r.SourceCurrent != "" || r.TargetCurrent != "" {
		fmt.Printf("  current balances md5 %s -> %s\n", r.SourceCurrent, r.TargetCurrent)
	}
	fmt.Printf("  elapsed %v\n", r.Elapsed.Round(time.Millisecond))
}
