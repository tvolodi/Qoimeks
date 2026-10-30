# Codes for SKUs and locations — design (built)

Status: **built** as Q-04 (pull requests 50–55, merged 2026-10-30); see "What differs from the design" at the end. Written as a design on 2026-10-27. Sources: portfolio request `qoimeks-codes-and-design.md` part 2,
decisions D-033 (codes) and D-036 (screens follow the design system: `ai-dala-portfolio/design-system`,
`RULES.md`, `demo.html` section "Codes"). Qoimeks is a pilot:
this is the smallest design that meets D-033.

## 1. Code types in version 1

| `code_type` | Meaning | Check on the server |
|---|---|---|
| `ean13` | EAN-13 barcode | 13 digits, check digit valid |
| `ean8` | EAN-8 barcode | 8 digits, check digit valid |
| `code128` | Code 128 barcode | 1–48 printable ASCII characters |
| `qr` | QR code (payload is text, e.g. `RI2-03/B8`) | 1–256 characters |
| `manual` | typed in the field, or a keyboard-style RFID/USB/Bluetooth reader (it "types" the tag number and presses Enter) | 1–64 characters, no check |

- The code is stored trimmed, case-sensitive, as read. No whitespace inside, no control characters.
- The app's scanner (`mobile_scanner`, already used by `qr_scanner.dart` for setup) reports the format;
  the app maps it to `code_type`. A typed or reader-entered value is `manual`; if it is 13 or 8 digits with a
  valid check digit the client may send `ean13`/`ean8` — the server does not insist.
- `code_type` is a label for display and validation only. It is **not** part of identity (see 2).
- Real RFID (reading tags by antenna) is later; it would be one more `code_type` (`rfid`), no schema change.

## 2. Data model

New migration `011_codes.sql` (design said 007; 007 was taken by `tenants_expiry`), applied per tenant schema like the ledger tables (not in `publicOnly`), so the
schema-per-tenant layout gives tenant scope with no `tenant_id` column.

```sql
create table codes (
    id          uuid primary key,              -- generated on the phone: idempotent assign
    object_type text not null check (object_type in ('sku', 'location')),  -- 'unit' added later
    object_id   bigint not null,
    code_type   text not null check (code_type in ('ean13','ean8','code128','qr','manual')),
    code        text not null check (code <> '' and code = btrim(code)),
    created_by  text not null check (created_by <> ''),   -- operator name label
    created_at  timestamptz not null default now(),
    removed_at  timestamptz,                    -- soft removal, see below
    removed_by  text
);
-- One code points to exactly one object per tenant: unique on the code alone, not on code + type.
create unique index codes_code_active_uq on codes (code) where removed_at is null;
create index codes_object_idx on codes (object_type, object_id) where removed_at is null;
```

Rules:
- **Unique code per tenant.** The unique index is on `code` only. The same digits cannot be an `ean13` on
  one SKU and a `qr` on a location. This is what makes "find object by code" return at most one object.
- **Several codes per object**: no unique constraint on `(object_type, object_id)`.
- **Several identical items share the SKU code**: ten pencils are one SKU; the SKU carries the code once.
- **Removal is a soft delete** (`removed_at`), so a removed code can be assigned elsewhere (the partial
  unique index ignores removed rows) and history of who had it is kept. Codes are catalog data, not ledger:
  they do not get the append-only trigger; `update` is allowed only for setting `removed_at/removed_by`
  (enforced in the store function, not by a trigger — pilot scope).
- **No foreign key** on `object_id` (it points to `skus` or `locations` depending on `object_type`); the store
  checks existence in the same transaction. A trigger-free design keeps `unit` addable (section 7).
- Existing `skus.barcode` (a nullable text column, shown in the catalog, never written today): migration
  copies each non-empty value into `codes` as `code_type = 'manual'` (or `ean13`/`ean8` when valid), and the
  API field `barcode` of the catalog is kept, filled from the first active code, until the app stops reading it.
  Column is not dropped in this change.

## 3. API

Same style as the existing API: JSON under `/api/*`, bearer access code, errors are `{"error": "<message>"}`
with the status from `storeError` (400 invalid, 404 not found, 409 conflict). New error responses add
**extra fields next to `error`** (existing clients ignore them); the message is Russian/English text for
people, `reason` is a stable machine value for the app to localize (ru/kk).

Object shape (`Code`):
```json
{"id":"<uuid>","object_type":"sku","object_id":17,"object_code":"SKU-00417","object_name":"Bolt M8×40",
 "code_type":"ean13","code":"4601234567890","created_by":"Aidar","created_at":"2026-11-02T08:15:00Z"}
```

| Call | Purpose | Success | Errors |
|---|---|---|---|
| `POST /api/codes` | assign a code | `201` + `Code`; `200` + same `Code` if the same `id` was already sent | 400, 404, 409 |
| `GET /api/codes?object_type=sku&object_id=17` | list active codes of an item (sorted by `created_at`) | `200` `{"codes":[Code,...]}` (empty list is fine) | 400 |
| `GET /api/codes/lookup?code=4601234567890` | find the object by code | `200` `Code` (includes the object's code and name) | 404 `{"error":"not found","reason":"code_not_found","code":"..."}` |
| `DELETE /api/codes/{id}?operator=Aidar` | remove a code | `200` `Code` with `removed_at`; repeated call → `200` same body (idempotent) | 404 |

`POST /api/codes` request:
```json
{"id":"<uuid generated on the phone>","object_type":"sku","object_id":17,
 "code_type":"ean13","code":"4601234567890","operator":"Aidar"}
```
- Validation (400, `reason":"invalid_code"`): unknown `object_type`/`code_type`, empty code, EAN check digit
  wrong for `ean13`/`ean8`, length limits above.
- Object missing or inactive: 404 `reason":"object_not_found"`.
- **Duplicate code, 409**, naming the owner:
```json
{"error":"code 4601234567890 is already used by SKU-00201 Washer M8",
 "reason":"code_in_use","code":"4601234567890",
 "owner":{"object_type":"sku","object_id":201,"object_code":"SKU-00201","object_name":"Washer M8"}}
```
  Assigning a code to the object that already has it is **not** an error: it returns `200` with the existing row
  (a retry with a new `id` after a lost response must not look like a duplicate).
- **Idempotency**: `id` is a client UUID like movements. Same `id` + same content → `200` existing row. Same `id`
  with different content → 409 `reason":"id_conflict"`, same text as the movement case ("already posted with
  different data"). Two phones racing for one code: the unique index decides; the loser gets `code_in_use`.
- `DELETE` takes the operator in the query (`DELETE` without a body is simplest for proxies); it is a label,
  like everywhere (no accounts).
- Tenant scope: handlers use `s.store(r)` which already selects the tenant's schema from the access code.
- Not in v1: `PATCH` (to change a code, remove and add), bulk assign (Excel covers bulk, section 5).

Offline: assigning needs the server (uniqueness is a server decision). The offline queue of stage 3 can later
queue `POST /api/codes` safely thanks to `id`; lookup could use a local cache of codes loaded with the catalog
(`GET /api/catalog` gains `codes` per item, or a `GET /api/codes/all` for the cache — decided at build time).

## 4. Screens

Components are those of the design system: Button (`primary`, `secondary`, `ghost`, `danger`), Input
(monospace for codes), Status chip, Banner, DataTable, Confirmation dialog, Empty/Error state, and the code
elements (code chip, scan button, viewfinder). All strings in ru and kk (+en), `l10n.dart`; labels above fields;
touch targets ≥ 48 px; status by icon + word + colour; works from 360 px.

**Mobile (Flutter)**
1. **Scan to find** — entry on the operation form next to the item field and as a button "Найти по коду / Код бойынша табу" on the home screen. Contents: scan button (amber primary, with icon and word),
   hint "or type / use a reader", `Input` (mono, autofocus-friendly, submit on Enter so keyboard-style readers work).
   Result: the form's item/location field is filled; on the home screen, a card with the object, its codes and
   balance. Nothing found → screen 6.
2. **Viewfinder** — full-screen camera with viewfinder frame (amber), label "Point at a barcode or QR", a
   "Type instead" secondary button, "Cancel". Reuses `qr_scanner.dart` result types (`ScanText`, `ScanCancelled`,
   `ScanPermissionDenied`, `ScanNoCamera`) extended with the format.
3. **Codes of an item** — opened from the item (SKU or location) screen. `DataTable`/list rows: code chip (type small caption + mono code) and a ghost "Remove" button; below it a secondary "Add a code" button. Empty
   state: "No codes yet" with the same button. Loading and error states per rules.
4. **Assign a code** — after "Add a code": choose "Scan" or "Type" (same Input as screen 1), code type `Select`
   (auto-detected from the scan, editable), primary "Save code". Success → toast "Code saved" and back to the list.
5. **Remove a code** — Confirmation dialog: "Remove code 4601234567890 from Bolt M8×40?", left "Keep" (safe),
   right danger "Remove code".
6. **Nothing found** — info Banner "Nothing found for `4600000000000`. Add it to an item or location." with
   primary "Add to an item" (opens item picker → screen 4 prefilled) and secondary "Scan again".
7. **Duplicate code** — error Banner (role alert) "Code already in use: `4601234567890` belongs to **Washer M8**. One code can point to one item only." with ghost "Open Washer M8" (goes to its code list) and secondary
   "Scan another". Text is built from `reason` + `owner`, localized in the app, not from the server's English line.
8. **Camera permission denied** — warning Banner "Camera is off. Allow the camera in phone settings, or type the code." with "Open settings" and "Type the code" buttons; the typed Input stays usable (the app never depends on the camera).

**Web (server-rendered `web.html`)**
- Item list/stock page gets a code column (mono chips, left-aligned) and a search box that accepts a typed or
  reader-entered code (Enter submits `GET /api/codes/lookup`); no camera on the web in v1.
- Per-item "Codes" panel: same list/add/remove as screens 3–5 with a typed Input and a Confirmation dialog;
  duplicate (7) and not-found (6) as inline Banners. Table scrolls inside its frame, never sideways on the page.
- Bookkeepers' use is mostly the Excel route (section 5); the web panel is for corrections.

## 5. Excel import / export

Small (include in the build):
- **Export** (`/api/stock/export`, `importer/export.go` `Write`): the export has one stock sheet of
  SKU × location rows. Add a column "Коды / Codes" after the existing ones with the SKU's active codes joined
  by `; `. **Location codes are not exported in v1** (there is no locations sheet). Read-only, no effect on
  `check`/`load`.
- **Import, optional column "Штрихкод"** (`importer/importer.go`). The workbook has one sheet per week, newest
  first, and each sheet has fixed columns A–G after the header row that starts with "Артикул". The importer looks
  in that header row for the cell with the text "Штрихкод"; if present, that column holds the SKU's code, if
  absent nothing changes (old workbooks import as before). Only one code per cell.
  - Parsing: `ImportSKU` (passed to `store.Import` as `[]store.ImportSKU`) gets a new field `Barcode string` (not `Code`: that name already holds the article); empty
    cell = no code. The code type is detected by shape (see below).
  - `check` (`Result.Warnings`, plain strings, changes nothing) warns about: a code longer than the length
    limit of section 2; a code owned by another SKU or location ("Строка 12: код 4600… уже занят товаром «X»");
    the same code on two different SKUs in the file. A 13/8-digit code with a wrong check digit is not an error;
    it is stored as `manual`. Warnings do not block `load`. Shape, length and in-file problems come from the
    parser; the "owned by another object" warning needs the database, so it comes from `store.Import` (which
    already runs in `check` as `DryRun`), returned in `ImportReport` (for example `CodeWarnings []string`) and
    appended to `remarks` in `api/import.go`.
  - `load`: in the same transaction as the stock rows, `store.Import` assigns each code with `created_by` =
    operator of the import. A code that is a duplicate (owned by another object) is **skipped with a warning;
    the row's stock is still loaded** — the workbook is the authority on balances, a bad code must not stop it.
  - SKU already has a different active code and the workbook gives a new one: the new code is **added as one
    more code** (several codes per object are allowed); nothing is removed.
  - The same code on two rows of the same SKU (an SKU can span several rows), or a code the SKU already has:
    a no-op (same object → 200 rule of section 3).
  - `load` applies exactly what was checked: the same parsing and the same skip rules.

Not small (leave out of v1):
- Several codes per cell/row, or a separate "Codes" sheet with `object, code_type, code` — needs a new sheet
  format and an agreed workbook layout with the bookkeepers (the workbook is the authority).
- **Import cancel does not revert codes** (consistent with "names/places written into the catalog are not
  reverted"); document it on the import page.
- Moving a code from one item to another via Excel (needs remove + assign in one checked step).
- Round-trip of `code_type`: the export writes only codes; import detects the type by shape (13/8 digits with
  valid check → EAN, else `manual`).

## 6. Backend effort

| Part | Size |
|---|---|
| Migration `011_codes.sql` + copy of `skus.barcode` | small |
| Store: assign (transaction: object exists, insert, map unique violation to a conflict that carries the owner), list, remove, lookup; EAN check digit helper | small–medium |
| API handlers, errors with `reason`/`owner`, tests with the tenant-schema harness (two tenants may use the same code) | medium |
| Catalog/export additions (codes in `GET /api/catalog`, export column) | small |
| Import optional column "Штрихкод": `ImportSKU.Code`, warnings in `check`, assignment in the `load` transaction | medium |
| Mobile: scan-to-find, code list, assign, error banners, l10n ru/kk, tests with a fake API | medium–large (largest part) |
| Web panel + search box | medium |
| Excel several-codes sheet, code move, offline lookup cache | large — later |

Suggested split of the build: (1) migration + store + API + tests; (2) mobile screens; (3) web panel and
export column; (4) import column. Each is a few hundred lines.

## 7. Unit items later

"This pencil with its own code" is a new kind of object under a SKU. Later migration, with no change to `codes`
or to the API shape:
1. `create table units (id bigint identity primary key, sku_id bigint not null references skus, serial text, status text not null default 'in_stock', location_id bigint references locations, created_at ...)`.
2. Widen the check: `alter table codes drop constraint codes_object_type_check, add constraint ... check (object_type in ('sku','location','unit'))` — a constraint change on a small table, no data rewrite.
3. The API already carries `object_type`; clients that only know `sku`/`location` receive a `unit` code only when
   they ask for units, and `lookup` returns `object_type: "unit"` plus the parent SKU in a new optional field
   (additive). The unique-on-`code` rule already prevents a unit code from colliding with a SKU code.
No breaking change because: `object_type` is text, not an enum or per-type table; `object_id` has no foreign key
tied to one table; uniqueness is on the code alone; and responses only gain optional fields. How movements
refer to a unit (a ledger change) is a separate design and deliberately not decided here.

## 8. Open questions

1. **Label printing** — not decided. Who prints (the bookkeeper on the web, or from the phone to a Bluetooth printer), what label size, and whether the server generates codes (e.g. internal `QX-000123` QR for locations) is open. Until decided, shelves use labels printed outside Qoimeks, and the app only reads them.
2. Should an operator be able to assign codes, or only the bookkeeper/manager? (No accounts today — any holder of the access code can.)
3. Hard delete vs soft delete of a removed code: soft chosen here; is the history needed?
4. Should scanning an unknown code inside the operation form offer "assign to the chosen item" directly (fewer taps)? Proposed yes; confirm in field.
5. Offline lookup: load all codes with the catalog (size?) — decide with the offline-queue design.
6. Do bookkeepers want the Excel column in v1, or is phone assignment enough?
7. Real RFID hardware: which reader and tag format, if any (the typed field covers keyboard-style readers only).
8. Case-insensitive match for `manual` / `code128` codes? v1 is case-sensitive.

## Left out on purpose
Label generation, accounts/permissions, unit items, offline assign, code editing (remove and add instead),
camera on the web.

## What differs from the design

Built in `server/internal/store/migrations/011_codes.sql`, `server/internal/store/codes.go`, `server/internal/api/codes.go`,
`mobile/lib/codes.dart` and `mobile/lib/code_find.dart`. Decisions of D-058 settle the open questions of section 8.

- **Migration number.** `011_codes.sql`, not `007` (007 is `tenants_expiry`). The `skus.barcode` copy is in SQL; with
  two SKUs holding the same barcode the lowest id keeps it. The copied rows have `created_by = 'migration'`.
- **Code types** are as designed: `ean13`, `ean8`, `code128`, `qr`, `manual`. The server checks EAN length and check digit
  for `ean13`/`ean8`, 48 printable ASCII characters for `code128`, 256 for `qr`, 64 for `manual`; no spaces or control
  characters in any. The app guesses the type (`guessCodeType`): EAN only when the check digit is right, else `manual`.
- **Codes in `GET /api/catalog`.** Decided at build time: each SKU and location in the catalog carries its active `codes`
  (omitted when empty), so there is no `GET /api/codes/all`. The export has a "Коды / Codes" column (SKU codes only).
- **Errors.** Every failure of the codes calls carries a `reason`: `code_in_use` (409, with `code` and `owner`),
  `id_conflict` (409), `invalid_code` (400), `invalid_request` (400), `object_not_found` (404 on assign),
  `code_not_found` (404 on lookup and remove). The duplicate error body is
  `{"error":..., "reason":"code_in_use", "code":..., "owner":{object_type, object_id, object_code, object_name}}`;
  the app builds its text from `reason` and `owner`.
- **Assign** is serialized per code by a transaction advisory lock; the unique index is the last guard. Assigning a code the
  object already has returns 200 with the existing row. `operator` and `id` are required.
- **Soft delete.** As designed: `removed_at`/`removed_by`, the code is freed for reuse, removing twice returns the same row.
  `DELETE /api/codes/{id}` needs `?operator=`, otherwise 400.
- **D-058 decisions:**
  - any holder of the access code may assign and remove codes (no roles);
  - no offline lookup in v1: scan-to-find and assign need the server; the catalog's codes are not used as an offline cache;
  - codes are case-sensitive, for every type;
  - RFID only through the typed field (`manual`), no antenna reading and no `rfid` type;
  - the Excel import column "Штрихкод" is the last column and may slip; it is optional and old workbooks import as before.
- **Unknown code in the operation form** (open question 4): the form offers to assign the unknown code to the item already
  chosen (`AssignTarget` in `code_find.dart`); the assign id is made per unknown code so a retry is the same assign.
- **Not built, as planned:** label printing, `PATCH`, bulk assign, a codes sheet in Excel, moving a code through Excel,
  unit items, location codes in the export, camera on the web.
