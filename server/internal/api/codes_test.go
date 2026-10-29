package api

import (
	"testing"

	"github.com/google/uuid"

	"qoimeks/server/internal/store"
)

type codeErrBody struct {
	Error  string          `json:"error"`
	Reason string          `json:"reason"`
	Code   string          `json:"code"`
	Owner  store.CodeOwner `json:"owner"`
}

func codeReq(objType string, objID int64, typ, code string) map[string]any {
	return map[string]any{"id": uuid.NewString(), "object_type": objType, "object_id": objID,
		"code_type": typ, "code": code, "operator": "Aidar"}
}

func catalogOf(t *testing.T, url, token string) store.Catalog {
	t.Helper()
	var cat store.Catalog
	call(t, "GET", url+"/api/catalog", token, nil, 200, &cat)
	return cat
}

// codesFlow runs assign and lookup, and the error reasons, as the bearer.
func codesFlow(t *testing.T, url, token string) {
	t.Helper()
	cat := catalogOf(t, url, token)
	sku, loc := cat.SKUs[0].ID, cat.Locations[0].ID
	const ean = "4006381333931"

	var c store.Code
	req := codeReq("sku", sku, "ean13", ean)
	call(t, "POST", url+"/api/codes", token, req, 201, &c)
	if c.Code != ean || c.ObjectID != sku || c.CreatedBy != "Aidar" {
		t.Fatalf("assigned: %+v", c)
	}
	call(t, "POST", url+"/api/codes", token, req, 200, &c) // same id: idempotent

	var got store.Code
	call(t, "GET", url+"/api/codes/lookup?code="+ean, token, nil, 200, &got)
	if got.ID != c.ID || got.ObjectCode != cat.SKUs[0].Code {
		t.Fatalf("lookup: %+v", got)
	}
	call(t, "POST", url+"/api/codes", token, codeReq("location", loc, "qr", "RI2-03/B8"), 201, nil)

	var list struct {
		Codes []store.Code `json:"codes"`
	}
	call(t, "GET", url+"/api/codes?object_type=sku&object_id="+itoa(sku), token, nil, 200, &list)
	if len(list.Codes) != 1 || list.Codes[0].Code != ean {
		t.Fatalf("list: %+v", list)
	}
}

func TestCodesOldSharedToken(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	if err := srv.Store.SeedDemo(t.Context()); err != nil {
		t.Fatal(err)
	}
	codesFlow(t, url, "shared")
}

func TestCodesTenantCodeAndDeviceKey(t *testing.T) {
	srv, base, _ := adminSetup(t, "shared")
	tn := newAdminTenant(t, base)
	seedCatalog(t, srv, tn.AccessCode, "T-1")
	codesFlow(t, base, tn.AccessCode)

	// A second tenant may use the same code on its own item.
	tn2 := newAdminTenant(t, base)
	seedCatalog(t, srv, tn2.AccessCode, "T-2")
	_, key := pairViaExchange(t, base, tn2)
	cat := catalogOf(t, base, key)
	call(t, "POST", base+"/api/codes", key, codeReq("sku", cat.SKUs[0].ID, "ean13", "4006381333931"), 201, nil)
	var got store.Code
	call(t, "GET", base+"/api/codes/lookup?code=4006381333931", key, nil, 200, &got)
	if got.ObjectCode != "T-2" {
		t.Fatalf("device lookup: %+v", got)
	}
	call(t, "GET", base+"/api/codes/lookup?code=4006381333931", "shared", nil, 404, nil)
}

func TestCodesDeviceKeyFlow(t *testing.T) {
	srv, base, _ := adminSetup(t, "shared")
	tn := newAdminTenant(t, base)
	seedCatalog(t, srv, tn.AccessCode, "T-1")
	_, key := pairViaExchange(t, base, tn)
	codesFlow(t, base, key)
}

func TestCodesErrorReasons(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	if err := srv.Store.SeedDemo(t.Context()); err != nil {
		t.Fatal(err)
	}
	cat := catalogOf(t, url, "shared")
	a, b := cat.SKUs[0], cat.SKUs[1]
	var e codeErrBody

	first := codeReq("sku", a.ID, "ean13", "4006381333931")
	call(t, "POST", url+"/api/codes", "shared", first, 201, nil)

	// Duplicate on another object names the owner.
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", b.ID, "manual", "4006381333931"), 409, &e)
	if e.Reason != "code_in_use" || e.Code != "4006381333931" || e.Owner.ObjectType != "sku" ||
		e.Owner.ObjectID != a.ID || e.Owner.ObjectCode != a.Code || e.Owner.ObjectName != a.Name || e.Error == "" {
		t.Fatalf("duplicate body: %+v", e)
	}
	// The same object with a new id is not an error.
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", a.ID, "ean13", "4006381333931"), 200, nil)

	// Same id, different content.
	clash := codeReq("sku", a.ID, "manual", "OTHER")
	clash["id"] = first["id"]
	call(t, "POST", url+"/api/codes", "shared", clash, 409, &e)
	if e.Reason != "id_conflict" {
		t.Fatalf("id conflict: %+v", e)
	}
	// Invalid.
	for _, bad := range []map[string]any{
		codeReq("sku", a.ID, "ean13", "4006381333932"),
		codeReq("sku", a.ID, "bogus", "X1"),
		codeReq("pallet", a.ID, "manual", "X1"),
		codeReq("sku", a.ID, "manual", "  "),
		codeReq("sku", a.ID, "manual", "has space"),
	} {
		call(t, "POST", url+"/api/codes", "shared", bad, 400, &e)
		if e.Reason != "invalid_code" {
			t.Fatalf("invalid %v: %+v", bad, e)
		}
	}
	noOp := codeReq("sku", a.ID, "manual", "X1")
	noOp["operator"] = ""
	call(t, "POST", url+"/api/codes", "shared", noOp, 400, &e)
	call(t, "POST", url+"/api/codes", "shared", "{not json", 400, &e)
	if e.Reason != "invalid_request" {
		t.Fatalf("bad json: %+v", e)
	}
	// Object missing.
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", 999999, "manual", "X2"), 404, &e)
	if e.Reason != "object_not_found" {
		t.Fatalf("missing object: %+v", e)
	}
	call(t, "POST", url+"/api/codes", "shared", codeReq("location", 999999, "manual", "X2"), 404, &e)

	// List.
	call(t, "GET", url+"/api/codes?object_type=sku", "shared", nil, 400, &e)
	call(t, "GET", url+"/api/codes?object_type=pallet&object_id=1", "shared", nil, 400, &e)
	if e.Reason != "invalid_request" {
		t.Fatalf("list: %+v", e)
	}
	var list struct {
		Codes []store.Code `json:"codes"`
	}
	call(t, "GET", url+"/api/codes?object_type=sku&object_id="+itoa(b.ID), "shared", nil, 200, &list)
	if list.Codes == nil || len(list.Codes) != 0 {
		t.Fatalf("empty list should be []: %+v", list)
	}

	// Lookup: missing, empty, case-sensitive.
	call(t, "GET", url+"/api/codes/lookup?code=NOPE", "shared", nil, 404, &e)
	if e.Reason != "code_not_found" || e.Code != "NOPE" {
		t.Fatalf("lookup missing: %+v", e)
	}
	call(t, "GET", url+"/api/codes/lookup?code=", "shared", nil, 400, &e)
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", a.ID, "manual", "AbC"), 201, nil)
	call(t, "GET", url+"/api/codes/lookup?code=abc", "shared", nil, 404, nil)

	// Remove: bad id, no operator, unknown, then idempotent and frees the code.
	call(t, "DELETE", url+"/api/codes/zzz?operator=A", "shared", nil, 400, &e)
	call(t, "DELETE", url+"/api/codes/"+first["id"].(string), "shared", nil, 400, &e)
	call(t, "DELETE", url+"/api/codes/"+uuid.NewString()+"?operator=A", "shared", nil, 404, &e)
	if e.Reason != "code_not_found" {
		t.Fatalf("remove unknown: %+v", e)
	}
	var rm store.Code
	call(t, "DELETE", url+"/api/codes/"+first["id"].(string)+"?operator=Aidar", "shared", nil, 200, &rm)
	if rm.RemovedAt == nil || rm.RemovedBy != "Aidar" {
		t.Fatalf("removed: %+v", rm)
	}
	call(t, "DELETE", url+"/api/codes/"+first["id"].(string)+"?operator=Other", "shared", nil, 200, &rm)
	if rm.RemovedBy != "Aidar" {
		t.Fatalf("repeat changed the row: %+v", rm)
	}
	call(t, "GET", url+"/api/codes/lookup?code=4006381333931", "shared", nil, 404, nil)
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", b.ID, "ean13", "4006381333931"), 201, nil)
}

func TestCatalogCarriesCodes(t *testing.T) {
	srv, url, _ := adminSetup(t, "shared")
	if err := srv.Store.SeedDemo(t.Context()); err != nil {
		t.Fatal(err)
	}
	cat := catalogOf(t, url, "shared")
	a, loc := cat.SKUs[0], cat.Locations[0]
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", a.ID, "ean13", "4006381333931"), 201, nil)
	call(t, "POST", url+"/api/codes", "shared", codeReq("sku", a.ID, "manual", "M-1"), 201, nil)
	call(t, "POST", url+"/api/codes", "shared", codeReq("location", loc.ID, "qr", "RI2-03/B8"), 201, nil)
	rem := codeReq("sku", cat.SKUs[1].ID, "manual", "GONE")
	call(t, "POST", url+"/api/codes", "shared", rem, 201, nil)
	call(t, "DELETE", url+"/api/codes/"+rem["id"].(string)+"?operator=A", "shared", nil, 200, nil)

	cat = catalogOf(t, url, "shared")
	if len(cat.SKUs[0].Codes) != 2 || cat.SKUs[0].Codes[0].Code != "4006381333931" || cat.SKUs[0].Codes[1].Code != "M-1" {
		t.Fatalf("sku codes: %+v", cat.SKUs[0].Codes)
	}
	if len(cat.Locations[0].Codes) != 1 || cat.Locations[0].Codes[0].CodeType != "qr" {
		t.Fatalf("location codes: %+v", cat.Locations[0].Codes)
	}
	if len(cat.SKUs[1].Codes) != 0 {
		t.Fatalf("removed code in catalog: %+v", cat.SKUs[1].Codes)
	}
	// Existing fields are unchanged and an item with no codes has no "codes" key.
	var raw struct {
		SKUs []map[string]any `json:"skus"`
	}
	call(t, "GET", url+"/api/catalog", "shared", nil, 200, &raw)
	if _, ok := raw.SKUs[1]["codes"]; ok {
		t.Fatalf("codes key on item without codes: %v", raw.SKUs[1])
	}
	for _, k := range []string{"id", "code", "name", "unit"} {
		if _, ok := raw.SKUs[0][k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}
