package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
	"qoimeks/server/internal/store/storetest"
)

func TestEANCheckDigitValid(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"4006381333931", true},
		{"4601234567891", false},
		{"96385074", true},
		{"96385075", false},
		{"400638133393", false},
		{"40063813339x1", false},
		{"", false},
	} {
		if got := store.EANCheckDigitValid(c.in); got != c.ok {
			t.Errorf("%q: got %v want %v", c.in, got, c.ok)
		}
	}
}

func skuID(t *testing.T, st *store.Store, n int) int64 {
	t.Helper()
	var id int64
	if err := st.Pool().QueryRow(context.Background(), `select id from skus where active order by id offset $1 limit 1`, n).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func assign(st *store.Store, sku int64, typ, code string) (store.Code, bool, error) {
	return st.AssignCode(context.Background(), store.NewCode{ID: uuid.New(), ObjectType: "sku", ObjectID: sku,
		CodeType: typ, Code: code, Operator: "Aidar"})
}

func TestAssignListLookupRemove(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	a, b := skuID(t, st, 0), skuID(t, st, 1)

	c, created, err := assign(st, a, "ean13", "4006381333931")
	if err != nil || !created || c.ObjectID != a || c.ObjectCode == "" || c.CreatedBy != "Aidar" {
		t.Fatalf("assign: %+v %v %v", c, created, err)
	}
	// Same object again with a new id: not an error, existing row.
	again, created, err := assign(st, a, "ean13", "4006381333931")
	if err != nil || created || again.ID != c.ID {
		t.Fatalf("reassign: %+v %v %v", again, created, err)
	}
	// Same id, same content: idempotent. Same id, other content: conflict.
	in := store.NewCode{ID: c.ID, ObjectType: "sku", ObjectID: a, CodeType: "ean13", Code: "4006381333931", Operator: "x"}
	if r, created, err := st.AssignCode(ctx, in); err != nil || created || r.ID != c.ID {
		t.Fatalf("retry: %+v %v %v", r, created, err)
	}
	in.Code = "ABC"
	in.CodeType = "manual"
	if _, _, err := st.AssignCode(ctx, in); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("id conflict: %v", err)
	}

	// Another object: conflict naming the owner.
	_, _, err = assign(st, b, "ean13", "4006381333931")
	var inUse *store.CodeInUseError
	if !errors.Is(err, store.ErrCodeInUse) || !errors.As(err, &inUse) || inUse.Owner.ObjectID != a || inUse.Owner.ObjectCode != c.ObjectCode || inUse.Owner.ObjectName != c.ObjectName {
		t.Fatalf("in use: %v", err)
	}

	// Several codes per object, listed oldest first; lookup is case-sensitive.
	if _, _, err := assign(st, a, "manual", "Tag-ab"); err != nil {
		t.Fatal(err)
	}
	list, err := st.ListCodes(ctx, "sku", a)
	if err != nil || len(list) != 2 || list[0].Code != "4006381333931" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if got, err := st.LookupCode(ctx, "Tag-ab"); err != nil || got.ObjectID != a {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if _, err := st.LookupCode(ctx, "tag-AB"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("case-sensitive lookup: %v", err)
	}

	// Soft removal keeps history and frees the code.
	rm, err := st.RemoveCode(ctx, c.ID, "Bolat")
	if err != nil || rm.RemovedAt == nil || rm.RemovedBy != "Bolat" {
		t.Fatalf("remove: %+v %v", rm, err)
	}
	if rm2, err := st.RemoveCode(ctx, c.ID, "Other"); err != nil || rm2.RemovedBy != "Bolat" {
		t.Fatalf("repeat remove: %+v %v", rm2, err)
	}
	if _, err := st.RemoveCode(ctx, uuid.New(), "Bolat"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("remove unknown: %v", err)
	}
	if _, err := st.LookupCode(ctx, "4006381333931"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removed code still found: %v", err)
	}
	if list, _ := st.ListCodes(ctx, "sku", a); len(list) != 1 {
		t.Fatalf("list after remove: %+v", list)
	}
	if n := count(t, st, `select count(*) from codes where code = '4006381333931'`); n != 1 {
		t.Fatalf("history rows: %d", n)
	}
	if r, created, err := assign(st, b, "ean13", "4006381333931"); err != nil || !created || r.ObjectID != b {
		t.Fatalf("reuse after removal: %+v %v %v", r, created, err)
	}
}

func TestAssignValidation(t *testing.T) {
	st := storetest.New(t)
	a := skuID(t, st, 0)
	for _, c := range []struct{ typ, code string }{
		{"ean13", "4601234567890"}, {"ean8", "96385075"}, {"manual", ""}, {"manual", "a b"},
		{"manual", "a\tb"}, {"code128", "é"}, {"bogus", "x"},
	} {
		if _, _, err := assign(st, a, c.typ, c.code); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s %q: %v", c.typ, c.code, err)
		}
	}
	if _, _, err := st.AssignCode(context.Background(), store.NewCode{ID: uuid.New(), ObjectType: "box", ObjectID: a, CodeType: "manual", Code: "x", Operator: "A"}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("object type: %v", err)
	}
	if _, _, err := assign(st, 999999, "manual", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing object: %v", err)
	}
	var locID int64
	if err := st.Pool().QueryRow(context.Background(), `select id from locations order by id limit 1`).Scan(&locID); err != nil {
		t.Fatal(err)
	}
	c, _, err := st.AssignCode(context.Background(), store.NewCode{ID: uuid.New(), ObjectType: "location", ObjectID: locID, CodeType: "qr", Code: "RI2-03/B8", Operator: "A"})
	if err != nil || c.ObjectType != "location" || c.ObjectCode == "" {
		t.Fatalf("location code: %+v %v", c, err)
	}
}

func TestSameCodeInTwoTenants(t *testing.T) {
	st := storetest.New(t)
	t1, _ := newTenant(t, st)
	t2, _ := newTenant(t, st)
	s1, s2 := tenantStore(t, st, t1), tenantStore(t, st, t2)
	for _, ts := range []*store.Store{s1, s2} {
		if err := ts.SeedDemo(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, created, err := assign(ts, skuID(t, ts, 0), "manual", "SHARED-1"); err != nil || !created {
			t.Fatalf("assign in tenant: %v %v", created, err)
		}
	}
}

func TestMigration011CopiesBarcodes(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	storetest.Exec(t, st, `
		delete from schema_migrations where name = '011_codes.sql';
		drop table codes;
		update skus set barcode = null;
		update skus set barcode = '4006381333931' where id = (select min(id) from skus);
		update skus set barcode = ' ABC-1 ' where id = (select id from skus order by id offset 1 limit 1);
		update skus set barcode = 'ABC-1' where id = (select id from skus order by id offset 2 limit 1);
		update skus set barcode = '96385074' where id = (select id from skus order by id offset 3 limit 1);
		update skus set barcode = '' where id = (select id from skus order by id offset 4 limit 1);`)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, st, `select count(*) from codes`); n != 3 {
		t.Fatalf("copied %d codes, want 3", n)
	}
	types := map[string]string{}
	rows, err := st.Pool().Query(ctx, `select code, code_type from codes`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c, ty string
		rows.Scan(&c, &ty)
		types[c] = ty
	}
	rows.Close()
	if types["4006381333931"] != "ean13" || types["96385074"] != "ean8" || types["ABC-1"] != "manual" {
		t.Fatalf("types: %v", types)
	}
	c, err := st.LookupCode(ctx, "ABC-1")
	if err != nil || c.ObjectID != skuID(t, st, 1) {
		t.Fatalf("lowest id keeps duplicate: %+v %v", c, err)
	}
}

func TestMigration011InTenantSchema(t *testing.T) {
	st := storetest.New(t)
	tn, _ := newTenant(t, st)
	ts := tenantStore(t, st, tn)
	if n := count(t, ts, `select count(*) from codes`); n != 0 {
		t.Fatalf("tenant codes: %d", n)
	}
}
