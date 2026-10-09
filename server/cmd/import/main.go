// Command import loads the bookkeepers' stock workbook into the catalog.
//
//	go run ./cmd/import [-sheet 18.09] [-balances] [-dry-run] file.xlsx
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"qoimeks/server/internal/config"
	"qoimeks/server/internal/importer"
	"qoimeks/server/internal/store"
)

func main() {
	sheet := flag.String("sheet", "", "sheet to read (default: the first one, which is the newest week)")
	balances := flag.Bool("balances", false, "also record the file's balances for SKUs and places the ledger has no stock for yet")
	dryRun := flag.Bool("dry-run", false, "only read the file and report, change nothing")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	config.LoadDotEnv(".env", "../.env")

	res, err := importer.Parse(flag.Arg(0), *sheet)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read the file:", err)
		os.Exit(1)
	}
	fmt.Printf("Sheet %q: %d SKUs, %d locations, %d remarks\n", res.Sheet, len(res.SKUs), len(res.Locations), len(res.Warnings))
	for _, w := range res.Warnings {
		fmt.Println("  " + w)
	}
	if *dryRun {
		return
	}

	ctx := context.Background()
	st, err := store.Open(ctx, config.Get(config.DefaultDatabaseURL, "DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot connect to the database:", err)
		os.Exit(1)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
	out, err := st.Import(ctx, res.Locations, res.SKUs, store.ImportOptions{
		PostBalances: *balances,
		Operator:     "импорт " + res.Sheet,
		Record: &store.ImportRecord{
			Who: "command line", Filename: filepath.Base(flag.Arg(0)), Sheet: res.Sheet,
			WithBalances: *balances, Remarks: len(res.Warnings),
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "import failed, nothing was changed:", err)
		os.Exit(1)
	}
	fmt.Printf("Saved: %d new SKUs, %d updated, %d new locations, %d opening balances\n",
		out.SKUsCreated, out.SKUsUpdated, out.LocationsCreated, out.BalancesPosted)
}
