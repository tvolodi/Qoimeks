package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

var ledgerTables = []string{"skus", "locations", "movements", "balances", "imports", "feedback"}

// ledgerFixture fills the default schema with movements, a reversal, an import and feedback.
func ledgerFixture(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	cat, err := st.Catalog(ctx)
	if err != nil || len(cat.SKUs) == 0 || len(cat.Locations) < 2 {
		t.Fatalf("catalog: %v %+v", err, cat)
	}
	sku, a, b := cat.SKUs[0].ID, cat.Locations[0].ID, cat.Locations[1].ID
	post := func(m store.NewMovement) store.Movement {
		t.Helper()
		m.ID, m.SKUID, m.Operator, m.Source = uuid.New(), sku, "тест", "keyboard"
		got, _, err := st.Post(ctx, m)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	post(store.NewMovement{Kind: store.KindReceipt, LocationID: a, Qty: 10})
	post(store.NewMovement{Kind: store.KindTransfer, LocationID: a, ToLocationID: &b, Qty: 4})
	wrong := post(store.NewMovement{Kind: store.KindIssue, LocationID: a, Qty: 1})
	if _, _, err := st.Reverse(ctx, wrong.ID, uuid.New(), "тест"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Import(ctx, []store.ImportLocation{{Code: "NEW-LOC", Name: "Новое"}},
		[]store.ImportSKU{{Code: "NEW-SKU", Name: "Новый"}},
		store.ImportOptions{Operator: "импорт", Record: &store.ImportRecord{Who: "t", Filename: "f.xlsx", Sheet: "1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendFeedback(ctx, store.Feedback{Kind: "user", Text: "привет"}); err != nil {
		t.Fatal(err)
	}
}

func tenantRows(t *testing.T, st *store.Store, schema string) int {
	t.Helper()
	n := 0
	for _, tb := range ledgerTables {
		n += count(t, st, `select count(*) from `+schema+`.`+tb)
	}
	return n
}

func TestCopyToTenantFullCopyEqualsSource(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)
	ctx := context.Background()

	rep, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Tables) != 6 || rep.SourceCurrent == "" || rep.SourceCurrent != rep.TargetCurrent {
		t.Fatalf("report: %+v", rep)
	}
	for _, tc := range rep.Tables {
		if tc.SourceRows != tc.TargetRows || tc.SourceSum != tc.TargetSum {
			t.Errorf("table %s differs in report: %+v", tc.Name, tc)
		}
	}
	for _, tb := range ledgerTables {
		s := count(t, st, `select count(*) from public.`+tb)
		d := count(t, st, `select count(*) from `+tn.SchemaName+`.`+tb)
		if s != d || s == 0 {
			t.Errorf("%s: source %d target %d", tb, s, d)
		}
	}
	// Row-level equality, ids and timestamps included.
	if n := count(t, st, `select count(*) from (select * from public.movements except select * from `+tn.SchemaName+`.movements) x`); n != 0 {
		t.Errorf("%d movements differ", n)
	}
	if n := count(t, st, `select count(*) from (select * from `+tn.SchemaName+`.balances except select * from public.balances) x`); n != 0 {
		t.Errorf("%d balances differ", n)
	}
	// A second run is refused.
	_, err = st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID})
	if !errors.Is(err, store.ErrTargetNotEmpty) {
		t.Fatalf("second run: %v", err)
	}
}

func TestCopyToTenantDryRunLeavesTargetEmpty(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)
	ctx := context.Background()

	rep, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || rep.Tables[3].SourceRows == 0 || rep.Tables[3].SourceRows != rep.Tables[3].TargetRows {
		t.Fatalf("dry run report: %+v", rep)
	}
	if n := tenantRows(t, st, tn.SchemaName); n != 0 {
		t.Fatalf("target has %d rows after a dry run", n)
	}
	// A dry run does not move the target's sequences either.
	var next int64
	if err := st.Pool().QueryRow(ctx, `insert into `+tn.SchemaName+`.locations (code) values ('X') returning id`).Scan(&next); err != nil || next != 1 {
		t.Fatalf("id after dry run = %d, %v", next, err)
	}
}

// analyzedTables counts the ledger tables of a schema that have statistics
// (reltuples is -1 until the first ANALYZE or VACUUM).
func analyzedTables(t *testing.T, st *store.Store, schema string) int {
	t.Helper()
	return count(t, st, `select count(*) from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = '`+schema+`' and c.relkind = 'r' and c.reltuples >= 0
		and c.relname in ('locations','skus','imports','movements','balances','feedback')`)
}

func TestCopyToTenantAnalyzesAfterRealRun(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)
	ctx := context.Background()

	if n := analyzedTables(t, st, tn.SchemaName); n != 0 {
		t.Fatalf("%d tenant tables analyzed before the copy", n)
	}
	rep, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Analyzed) != 6 {
		t.Fatalf("analyze report: %+v", rep.Analyzed)
	}
	for _, a := range rep.Analyzed {
		if a.Err != nil {
			t.Errorf("analyze %s: %v", a.Name, a.Err)
		}
	}
	if n := analyzedTables(t, st, tn.SchemaName); n != 6 {
		t.Errorf("%d of 6 tenant tables have statistics after the copy", n)
	}
}

func TestCopyToTenantDryRunDoesNotAnalyze(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)

	rep, err := st.CopyToTenant(context.Background(), store.CopyOptions{TenantID: tn.ID, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Analyzed) != 0 {
		t.Errorf("dry run reports analyze: %+v", rep.Analyzed)
	}
	if n := analyzedTables(t, st, tn.SchemaName); n != 0 {
		t.Errorf("%d tenant tables analyzed by a dry run", n)
	}
}

func TestCopyToTenantRefusals(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)
	ctx := context.Background()

	if _, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: uuid.New()}); err == nil {
		t.Error("unknown tenant accepted")
	}
	if _, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID, SourceSchema: "no_such_schema"}); err == nil {
		t.Error("unknown source schema accepted")
	}
	if _, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID, SourceSchema: tn.SchemaName}); err == nil {
		t.Error("copy onto itself accepted")
	}
	if _, err := st.Pool().Exec(ctx, `insert into `+tn.SchemaName+`.locations (code) values ('X')`); err != nil {
		t.Fatal(err)
	}
	_, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID})
	if !errors.Is(err, store.ErrTargetNotEmpty) {
		t.Fatalf("non-empty target: %v", err)
	}
	if n := count(t, st, `select count(*) from `+tn.SchemaName+`.movements`); n != 0 {
		t.Errorf("movements copied despite refusal: %d", n)
	}
}

func TestCopyToTenantMismatchAborts(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)
	// A trigger that alters quantities on the way in, as a faulty copy would.
	storetest.Exec(t, st, `
		create function `+tn.SchemaName+`.skew() returns trigger language plpgsql as $$
		begin new.qty := new.qty + 1; return new; end $$;
		create trigger skew before insert on `+tn.SchemaName+`.movements
		for each row execute function `+tn.SchemaName+`.skew();`)
	ctx := context.Background()

	rep, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID})
	if !errors.Is(err, store.ErrCopyMismatch) {
		t.Fatalf("err = %v", err)
	}
	if rep.Tables[3].Name != "movements" || rep.Tables[3].SourceSum == rep.Tables[3].TargetSum {
		t.Errorf("report does not show the movements mismatch: %+v", rep.Tables[3])
	}
	if n := tenantRows(t, st, tn.SchemaName); n != 0 {
		t.Fatalf("target has %d rows after an aborted copy", n)
	}
}

func TestCopyToTenantSequencesContinue(t *testing.T) {
	st := storetest.New(t)
	ledgerFixture(t, st)
	tn, _ := newTenant(t, st)
	ctx := context.Background()
	if _, err := st.CopyToTenant(ctx, store.CopyOptions{TenantID: tn.ID}); err != nil {
		t.Fatal(err)
	}
	for _, tb := range []struct{ table, col string }{
		{"skus", "id"}, {"locations", "id"}, {"balances", "id"}, {"imports", "id"}, {"feedback", "id"}, {"movements", "seq"},
	} {
		var next, max int64
		if err := st.Pool().QueryRow(ctx, `select coalesce(max(`+tb.col+`), 0) from `+tn.SchemaName+`.`+tb.table).Scan(&max); err != nil {
			t.Fatal(err)
		}
		if err := st.Pool().QueryRow(ctx, `select nextval(pg_get_serial_sequence('`+tn.SchemaName+`.`+tb.table+`', '`+tb.col+`'))`).Scan(&next); err != nil {
			t.Fatal(err)
		}
		if next <= max {
			t.Errorf("%s.%s: next %d is not above max %d", tb.table, tb.col, next, max)
		}
	}
	// A real operation in the tenant posts without an id clash.
	ts := tenantStore(t, st, tn)
	cat, err := ts.Catalog(ctx)
	if err != nil || len(cat.SKUs) == 0 {
		t.Fatalf("tenant catalog: %v", err)
	}
	if _, _, err := ts.Post(ctx, store.NewMovement{ID: uuid.New(), Kind: store.KindReceipt, SKUID: cat.SKUs[0].ID,
		LocationID: cat.Locations[0].ID, Qty: 1, Operator: "тест", Source: "keyboard"}); err != nil {
		t.Fatalf("post after copy: %v", err)
	}
}
