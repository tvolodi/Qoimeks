# Pairing a phone to a tenant with per-device keys — design (built)

Status: built (Q-02), design of 2026-10-28; owner decisions D-049 apply: same provisioning token for pairing admin calls; at most 5 pending codes and 50 devices per tenant, code lifetime 10 minutes; revoke only through the admin API; device key stored as today (SharedPreferences, `Authorization: Bearer qdk_...`); plain http allowed with a warning; revoked devices kept; per-code counter as in section 4; Client 1 pairing optional and later; Option B later. The text below is the original design. Source: portfolio request (owner direction, QA only per D-042), decision D-045
(pairing design before Stage 3), D-020 (only a hash of a tenant's access code is stored). Qoimeks is a pilot: this is the smallest design that meets the request.

Marking: **VERIFIED** = read in the code of this repository on 2026-10-28. **PROPOSED** = a decision of this design,
open to change by the owner. Every code, key and id in the examples is obviously fake.

## 0. Problem and scope

- **Version 1 (Client 1)**: one shared `ACCESS_TOKEN`, all local users have full access. **Nothing changes.**
- **Multi-tenant version**: the user signs in on the front door (the ai-dala.com site, `ai-dala.next`, Google
  sign-in done there). The phone must then be set up **without copy and paste**, or the device must authorize itself.
- **Constraint D-020**: the server stores only `access_hash` of a tenant's code, so it can never hand out the shared
  code later. VERIFIED: `store/tenant.go` `CreateTenant` stores `hashCode(code)` (sha256) and returns the clear code
  only when `created=true`; `RotateTenantCode` returns the new code once; migration 006 dropped the clear column.
  So the pairing flow must give the phone **its own credential**, not a copy of the tenant code.

**Option A (this design)**: the front door asks Qoimeks for a one-time pairing code; the phone exchanges it for a
per-device key tied to the tenant; keys are revocable per device. The shared tenant code and `ACCESS_TOKEN` keep working.
**Option B (later, section 8)**: the device shows a short code and the signed-in user approves it on a web page.

## 1. What exists today (VERIFIED)

- `api.go` `requireToken` guards paths under `/api/` only. Order: (1) the bearer equals the configured `AccessToken`
  (`subtle.ConstantTimeCompare`) → default schema, or the mapped tenant when `AccessTokenTenant` is set
  (`s.Store.Tenant`, then `serveTenant`); (2) otherwise a bearer is hashed and looked up with `store.TenantByCode`
  → `serveTenant`; (3) otherwise `401 {"error":"access code is missing or wrong"}`; with no `AccessToken` set and no
  matching tenant code the default schema stays open.
- `serveTenant` applies the status rules: `expired` → 403 `tenant expired`; `suspended` → 403 `tenant suspended`;
  active → `s.tenantStore(schema)` is put in the request context, handlers use `s.store(r)`.
- `admin.go` `adminRoutes`: the provisioning API lives under `/admin/v1/...` (outside `/api/`, so `requireToken` does
  not touch it), exists only when `ProvisioningToken` is set (else 404), guarded by bearer + a single global
  `rateLimiter` (30 requests/minute). Errors are `{"error":"<code>","message":"<text>"}` (`writeAdminError`), different
  from the app API's `{"error":"<text>"}`. Existing routes: `POST /admin/v1/tenants`, `.../{id}/suspend|resume|expire|rotate-code`,
  `GET .../{id}`.
- `ratelimit.go` `rateLimiter`: n events per rolling window, **one counter, not per key**, in memory, clock injectable.
- Registry: `tenants(tenant_id, name, language, tier, owner_email, schema_name, access_hash bytea unique, status,
  created_at, updated_at, expires_at)` in the default schema only (`migrate.go` `publicOnly`); latest migration is
  `009_provider_settings.sql`. (`codes-design.md` says 007 for its migration; numbers are taken in order of merging.)
- No code reads `X-Forwarded-For`/`CF-Connecting-IP`/`X-Real-IP`; `r.RemoteAddr` behind the host's nginx is the
  proxy's address (the Docker bridge gateway, not loopback). A per-IP limit therefore needs a small trusted-proxy
  rule (section 4).
- Mobile: `settings.dart` keeps `serverUrl`, `accessCode`, `operator`, `lang` in `SharedPreferences` (plain, not secure
  storage). `api.dart` `ApiClient` sends `Authorization: Bearer <accessCode>` plus `X-App-Version`, `X-Device`
  (model label), `X-Operator`. `setup_qr.dart` `parseSetupQr` accepts only `qoimeks://setup?url=<enc>[&code=<enc>]`,
  `url` must be http(s) with a host, returns `null` otherwise, never throws. `qr_scanner.dart` has
  `ScanText/ScanCancelled/ScanPermissionDenied/ScanNoCamera`. `cors` already allows the `Authorization` header.
- The web page (`web.html`) builds its setup QR in a pure function `setupPayload(origin, code)` that the Go test runs
  under node; the access code lives in `sessionStorage`.

## 2. Concepts (PROPOSED)

| Thing | What it is | Secret? | Stored |
|---|---|---|---|
| Pairing code | `K7M2-9QXA`-style, single use, ~10 min | yes, short-lived | sha256 only |
| Device key | `qdk_` + 32 random bytes in base32 (about 52 characters), one per phone | yes, long-lived | sha256 only, shown once |
| Device | a row: label, operator, created/last seen/revoked | no | clear |

- A device key behaves like a tenant code for **its own tenant only**: full access to `/api/*`, same status rules
  (suspended/expired tenant → 403). Per-device roles are not part of this design (no accounts, see qoimeks-domain).
- The key has a recognisable prefix `qdk_` so the middleware picks the right lookup without trying both, and so
  the history scan / log scrubbing can recognise it. Keys are never logged and never returned again.

## 3. API (PROPOSED)

Styles follow what exists: admin calls under `/admin/v1/` with the provisioning bearer and the
`{"error":"<code>","message":"<text>"}` shape; the app-facing call has its own public path.

### 3.1 Create a pairing code (admin)

`POST /admin/v1/tenants/{tenant_id}/pairings` — bearer = provisioning token. Called by the front door's server.

Request (all fields optional):
```json
{"label":"Warehouse phone 2","ttl_seconds":600}
```
`label` ≤ 80 characters, shown later in the device list as the suggested name; `ttl_seconds` 60–900, default 600.

Response `201`:
```json
{"pairing_id":"4f0c2d1e-0000-4000-8000-000000000001",
 "code":"K7M2-9QXA",
 "expires_at":"2026-11-02T09:10:00Z",
 "url":"https://qoimeks.example",
 "pair_uri":"qoimeks://pair?url=https%3A%2F%2Fqoimeks.example&pair=K7M2-9QXA"}
```
- `code` is returned **only here** (only its hash is stored, like `CreateTenant`). `url` is the server's public URL
  (the existing `defaultPublicURL` / public-URL setting of `admin.go`). `pair_uri` is what the QR carries.
- Not idempotent: each call is a new code. A pending code does not need to be cancelled before a new one is made.

| Status | `error` | When |
|---|---|---|
| 201 | | created |
| 400 | `invalid_request` | bad JSON, label too long, `ttl_seconds` out of range |
| 401 | `unauthorized` | provisioning token missing or wrong |
| 404 | `not_found` | unknown tenant |
| 409 | `conflict` | tenant suspended or expired (message says which) — no pairing for a tenant that cannot be used |
| 409 | `too_many_pending` | the tenant already has 5 unused, unexpired codes, or 50 active devices |
| 429 | `rate_limited` | the existing admin limiter (30/min), with `Retry-After` |

`POST /admin/v1/tenants/{tenant_id}/pairings/{pairing_id}/cancel` → `200 {"pairing_id":"...","cancelled_at":"..."}`
(repeat → same 200; unknown → 404). It lets the front door withdraw a code when the user closes the dialog.

### 3.2 Exchange the code (public, rate limited)

`POST /pair/v1/exchange` — **no bearer**. Path is outside `/api/` on purpose: VERIFIED that `requireToken` only guards
`/api/`, and the caller has no credential yet. It exists only when pairing is enabled (the same rule as admin: no
`ProvisioningToken` → 404).

Request:
```json
{"code":"k7m2 9qxa","device_label":"Samsung A15","operator":"Aidar","app_version":"0.2.0"}
```
`code` is normalised (section 4), so typed lowercase, spaces or a missing hyphen work. `device_label` ≤ 80 (the app
sends the same model string as `X-Device`); `operator` ≤ 80 and optional (a label, like everywhere).

Response `200`:
```json
{"device_id":"7a1b3c5d-0000-4000-8000-000000000002",
 "device_key":"qdk_FAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKEFAKE",
 "tenant":{"name":"Demo warehouse","language":"ru"}}
```
`Cache-Control: no-store`. The key is in this one response only.

| Status | `error` | When |
|---|---|---|
| 200 | | paired; the code is now used up |
| 400 | `invalid_request` | bad JSON, missing `code`/`device_label`, field too long (the code is **not** consumed) |
| 400 | `invalid_pairing_code` | unknown, expired, already used or cancelled — one answer for all four, so the response says nothing about which codes exist |
| 403 | `tenant_unavailable` | the code was valid but the tenant is suspended/expired (the code is consumed) |
| 429 | `rate_limited` | per-IP or global failure limit (section 4); `Retry-After` in seconds |

Lost response: the code is consumed and the key was never seen. The user makes a new code (cheap). A replay of the
same code is not offered because the key cannot be re-created from its hash.

### 3.3 Devices (admin)

`GET /admin/v1/tenants/{tenant_id}/devices` → `200`:
```json
{"devices":[{"device_id":"7a1b3c5d-0000-4000-8000-000000000002","label":"Samsung A15","operator":"Aidar",
  "app_version":"0.2.0","created_at":"2026-11-02T09:02:11Z","last_seen_at":"2026-11-02T09:40:00Z","revoked_at":null}]}
```
Newest first; revoked devices are listed with `revoked_at`. The key (or any part of it) is never listed.
Errors: 401, 404 (unknown tenant), 429.

`POST /admin/v1/tenants/{tenant_id}/devices/{device_id}/revoke` → `200` the device with `revoked_at` set. Repeat → `200`
same body (idempotent). Unknown device, or one of another tenant → `404 not_found`. Revoking takes effect on the next
request: the middleware looks the key up on every call (no cache in v1), so there is no grace period to explain.

(Same convention as the existing `suspend`/`resume`: a `POST` verb under the resource.)

### 3.4 How a device key authenticates

`Authorization: Bearer qdk_...` — **the same header the app sends today** (VERIFIED `api.dart`, `cors` allow-list). No new
header, no change in `ApiClient` beyond the value. (`X-Device` already exists as a model *label*; a second
`X-Device-Key` header would confuse the two and need a CORS change.)

In `requireToken`, before the existing tenant-code lookup:
1. bearer starts with `qdk_` → look up `sha256(bearer)` in `devices`;
2. found, not revoked → load its tenant, `serveTenant` (same 403 rules); `last_seen_at` is written at most once per
   5 minutes per device (not on every request);
3. found but revoked → `401 {"error":"device revoked","reason":"device_revoked"}` (extra field next to `error`, as in
   `codes-design.md` section 3; old clients ignore it, the new app shows "this phone was disconnected, pair it again");
4. not found → the usual `401 access code is missing or wrong`.

Device keys are **not** accepted by `/admin/v1/*` and cannot create pairings or manage devices (no way to mint more
keys from a lost phone). The configured `ACCESS_TOKEN` and tenant codes are unaffected: with no `qdk_` prefix the
existing code path runs exactly as before.

## 4. Pairing code, expiry and brute-force limits (PROPOSED)

**Alphabet.** 30 characters: digits `2–9` and the letters `A–Z` without `I L O U` — i.e. no `0/O`, `1/I/L`, and no `U`
(read as `V`). 8 characters shown as `XXXX-XXXX`. Entropy: log2(30) = 4.91 bits per character × 8 = **39.3 bits**
(30^8 ≈ 6.6 × 10^11). Typed input is normalised: upper-cased, spaces and hyphens removed. Look-alikes are not mapped:
an input with `0`, `O`, `1`, `I`, `L` or `U` is simply invalid and
counts as a failed attempt. Generated with `crypto/rand`, rejection sampling (no modulo bias).

**Lifetime.** Default 10 minutes (`ttl_seconds` 60–900). The check is `expires_at > now()` in the same SQL statement that
consumes the code. Expired rows are deleted 7 days after expiry (pairings are not ledger data).

**Single use.** One statement in one transaction: `update pairings set used_at = now() where code_hash = $1 and used_at is
null and cancelled_at is null and expires_at > now() returning tenant_id, ...`, then `insert into devices ...` in the same
transaction. Two phones racing for one code: the row update has one winner; the loser gets `invalid_pairing_code`.
No separate application lock is needed; the row lock of the `update` is the lock.

**Limits on exchange** (the endpoint is public, so these are the defence; counters are in memory like the admin limiter, so
a server restart resets them — acceptable for a pilot with one instance):

| Limit | Value | Effect |
|---|---|---|
| Failed attempts per client IP | 10 per 10 minutes | the IP gets `429` with `Retry-After` until the oldest failure leaves the window (lockout of at most 10 minutes); a success does not reset it |
| All attempts per client IP | 30 per 10 minutes | bounds noise from one address |
| Failed attempts, whole server | 300 per 10 minutes | exchange answers `429` for everyone for the rest of the window (bounds distributed guessing; a loud attacker can delay legitimate pairing, never succeed) |
| Pending codes per tenant | 5 | bounds how many valid targets exist at once |
| Devices per tenant | 50 | bounds key accumulation |

**Why there is no "failed attempts per code" counter.** A wrong guess does not match any row, so it cannot be tied to a
code; counting "per code" would need the code to carry a public lookup id, which the typed entry cannot (the user would
type two things). The same effect is reached by: single use + 10-minute life + at most 5 live codes per tenant + the IP
and global failure limits. If the owner wants an explicit per-code counter, the QR could carry `pair=<id>.<code>`; typed
entry would still use the bare code (open question 5).

**Odds.** With 100 live codes on the server at once (a generous pilot figure) one guess succeeds with probability
100 / 6.6 × 10^11 ≈ 1.5 × 10^-10. At the global cap of 300 failures per 10 minutes (43 200 guesses a day) the chance of
**any** hit in a day is about 6.5 × 10^-6, and the codes are dead after 10 minutes anyway.

**Client IP.** VERIFIED in this repository: nothing reads forwarding headers today, so `RemoteAddr` is the proxy.
VERIFIED by infra (portfolio-manager, 2026-10-28):
- nginx sets `X-Real-IP` to `$remote_addr` and always overwrites it, so a client cannot spoof it. With `real_ip`
  configured for all Cloudflare ranges, `$remote_addr` is the true visitor IP.
- `X-Forwarded-For` holds the client's own value with the peer appended: only the **last** entry is trustworthy.
- `CF-Connecting-IP` is **not** cleared by the qoimeks vhosts: never trust it.
- The app container's direct peer is **not** `127.0.0.1`. nginx reaches the container through Docker's port mapping,
  so the peer is the Docker bridge gateway of the container's compose network (roughly `192.168.208.x` in prod,
  `10.203.119.x` in QA; not measured). A rule "trust headers only from loopback" would therefore never trust them.
- Ports: the container is published on `127.0.0.1:3113` (prod) and `127.0.0.1:3100` (QA); ufw is default-deny; each
  container is alone on its compose network with its own database. So nothing but the host's nginx reaches the port.

PROPOSED rule: a small helper returns the client IP from `X-Real-IP` **when the direct peer (`RemoteAddr`) is inside a
trusted range**, configured by the env variable `TRUSTED_PROXY_CIDRS` (comma-separated CIDRs; default: the compose
network gateway range plus loopback); otherwise it returns `RemoteAddr`. It ignores `CF-Connecting-IP` and the
leftmost `X-Forwarded-For` entries. A missing or unparsable `X-Real-IP` also falls back to `RemoteAddr`. Infra can
measure the exact peer address when the build starts, to set the default. (Open question 4 is closed.)

**At rest.** Pairing codes and device keys are stored as `sha256` hashes, exactly as `tenants.access_hash` (VERIFIED
`hashCode`). The device key has 256 bits of entropy, so an unsalted hash is sound. The pairing code has only 39 bits, so
its hash could be brute-forced offline if the table leaked — but only while the code is live (≤ 15 minutes), and used
or expired codes are worthless; this is accepted.

## 5. Storage (PROPOSED)

New migration (next free number at build time, e.g. `010_pairing.sql`), **public schema only** — add to `publicOnly` in
`migrate.go` like the registry: the middleware must find a device and its tenant before it knows the tenant schema, and
a tenant schema must not see other tenants' devices. Both tables carry `tenant_id`.

```sql
create table device_pairings (
    pairing_id   uuid primary key,
    tenant_id    uuid not null references tenants (tenant_id),
    code_hash    bytea not null unique,
    label        text not null default '',
    created_at   timestamptz not null default now(),
    expires_at   timestamptz not null,
    used_at      timestamptz,
    cancelled_at timestamptz
);
create index device_pairings_tenant_idx on device_pairings (tenant_id, expires_at);

create table devices (
    device_id    uuid primary key,
    tenant_id    uuid not null references tenants (tenant_id),
    key_hash     bytea not null unique,        -- sha256 of the device key; the key itself is never stored
    label        text not null,
    operator     text not null default '',
    app_version  text not null default '',
    pairing_id   uuid references device_pairings (pairing_id),
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz,
    revoked_at   timestamptz
);
create index devices_tenant_idx on devices (tenant_id, created_at desc);
```
- Devices are **not** deleted; revoking sets `revoked_at` (kept so the 401 can say `device_revoked` and the owner sees
  history). `update` is only for `revoked_at`, `last_seen_at`, `app_version` (enforced in the store function, as
  for `codes` in `codes-design.md`; these are registry data, not ledger — the append-only rules of `movements`,
  `balances` and `imports` are untouched).
- Nothing is added to the tenant schemas, so the ledger, its triggers and its queries stay as they are.
- The device key is created in the store function with `crypto/rand` and returned to the handler once, the same
  pattern as `newCode`/`CreateTenant`.

## 6. Where the pairing code is created, and the front-door call (PROPOSED)

1. The user signs in on the front door (Google sign-in, done entirely on `ai-dala.next`; Qoimeks has no OAuth).
2. The user presses "Add a phone". The **server side** of the site (a route handler, never the browser) calls
   `POST /admin/v1/tenants/{tenant_id}/pairings` with the provisioning token it already holds for tenant creation, using the
   tenant id of the signed-in user.
3. The site shows the QR of `pair_uri`, the text `K7M2-9QXA`, the server address, and a countdown to `expires_at`
   (and offers "new code"). It may call `.../cancel` when the dialog closes.
4. The site's device page uses `GET .../devices` and `.../revoke`.

The provisioning token never reaches a browser or a phone. The QR carries a code valid for ~10 minutes and one use — much
safer than the current web page QR, which can carry the shared access code itself (VERIFIED `setupPayload`).
Qoimeks' own web page does not mint pairing codes in v1: it has no provisioning token and no sign-in beyond the shared
code.

## 7. What changes in the app and the web page (PROPOSED)

**Payload.** `qoimeks://setup?url=..&code=..` stays valid and untouched (`parseSetupQr` keeps its behaviour and tests). New
form: `qoimeks://pair?url=<enc>&pair=<enc code>`. A new parser in `setup_qr.dart` (`parsePairQr` returning a `PairPayload`
with `url` and `code`, same rules: scheme `qoimeks`, host `pair`, `url` http(s) with a host, `null` otherwise, never throws,
`toString` hides the code) and one dispatcher `parseScannedQr(raw)` → `SetupPayload | PairPayload | null` for the scanner.

**Scanner.** `qr_scanner.dart` is unchanged (it returns text); the Settings flow calls the dispatcher on `ScanText`.
`SetupPayload` fills the form as today. `PairPayload` calls the exchange at once and then saves.

**Settings screen.** Next to "Scan QR": a "Connect with a pairing code / Қосылу коды" section with one field (mono, hint
`K7M2-9QXA`, upper-casing as the user types, accepts hyphen or no hyphen) and a "Connect" button. If the server address is
empty the form shows the address field too (typed entry); the web build with `DEFAULT_SERVER_URL` has it prefilled.
Success: the app stores `serverUrl` and puts the device key into the existing `accessCode` field; the field is shown
masked with a status line "Paired as «Samsung A15»"; the operator name is kept if already typed. Errors are
localized from `error`/`reason`, ru and kk (+en): wrong or used code, "ask for a new code" (expired, same message),
wait N minutes (429), tenant unavailable, no network (what still works: nothing is lost, try again). All strings go
in `l10n.dart` in ru and kk — first-class.

**Storing and sending the key.** PROPOSED: in the same place as the access code today (`SharedPreferences`, VERIFIED
`settings.dart`) and sent by the unchanged `ApiClient` as `Authorization: Bearer qdk_...`. So `isComplete`, feedback
and the update check need no change. Moving secrets to Android secure storage (a new package) belongs to Stage 3
item 4 and would move both the access code and the device key at once (open question 6). On `401 reason=device_revoked`
the app shows a non-dismissible banner "This phone was disconnected" with a button to the pairing form; its data on the
server is untouched.

**Typed entry / iPhone web / no camera.** The pairing form needs no camera and no URL typing when the build has a default
server address: the user reads `K7M2-9QXA` from the front door's screen and types it. The exchange works from the web
build because the CORS middleware already allows `POST` and the headers the call needs (`Content-Type`). The key then
lives in the browser's local storage (the same as `SharedPreferences` on web). The front door can also show the code as a
link to the web build with the code prefilled — a later nicety, not in v1.

**Qoimeks web page (`web.html`)**: unchanged in v1. It keeps showing the setup QR (`setupPayload`) for the existing flows.

## 8. Option B — device sign-in (optional, later; PROPOSED outline)

The app (with no credential) calls `POST /pair/v1/device-requests {device_label}` and shows a short code such as
`W4N8-3RTZ` plus a poll id; it polls `GET /pair/v1/device-requests/{id}` (public, rate limited as the exchange). The
signed-in user opens the front door, types or scans the code, picks the tenant and approves with
`POST /admin/v1/tenants/{id}/device-requests/approve {code}`; the next poll returns the device key once (kept hashed
and delivered one time, then wiped). Reuses `devices`, the key format, revocation and the middleware branch; adds one
table (`device_requests`, TTL 10 minutes), polling, and a front-door screen. It is better for phones that can show but
not scan, and is the base for a future OAuth device-flow. Not needed until pairing by code shows a reason. Brute-force
reasoning is the same, except that the 30^8 code is guessed by the *user's* side (approval), so the limit moves to
the approve call.

## 9. Effect on Client 1 and on the migration plan

- **Client 1 is unaffected.** VERIFIED: the middleware order puts the configured `ACCESS_TOKEN` (and its
  `AccessTokenTenant` mapping, PR #28) and tenant codes first-class; the device branch only fires for a bearer
  with the `qdk_` prefix, which no existing credential has (tenant codes are 64 hex characters, the token is the
  owner's own string; a token that happened to start with `qdk_` would be matched by the token branch first — configure
  tokens without that prefix). Without `ProvisioningToken` the new routes do not exist. No schema change in `public`
  tables used by Client 1, no change in the default schema, no change in the API of the app.
- **Client 1 migration plan** (`project/client1-migration-plan.md`, step C5 "the old token resolves to the new tenant"):
  nothing in it depends on this design. After the migration the old token maps to the Client 1 tenant and **keeps
  working indefinitely** (decision 2 of the plan). Devices can be paired to that tenant **later**, one by one, using the
  tenant's id: the owner (or a small operator script holding the provisioning token) calls the create call; there
  is no front door sign-in for Client 1, so pairing is optional there. If a phone gets lost, Client 1 can then revoke one device
  instead of rotating the shared token for everybody. Suggested line for that plan (not edited by this task):
  "Pairing (D-045) is optional for Client 1 after the window; the old token stays valid."
- Because the device key is tied to a `tenant_id` and not to a schema, migrating the Client 1 data (copy to a new
  schema) does not invalidate keys issued before or after.

## 10. What is small and what is not

| # | Piece | Size |
|---|---|---|
| 1 | Migration (two tables, `publicOnly`) + store functions: create/cancel pairing, exchange in one transaction, device create/list/revoke/touch, tests with the storetest harness | medium |
| 2 | Code generator (alphabet, rejection sampling, normalisation) + key generator | small |
| 3 | Keyed rate limiter (per IP, failure windows, global cap) + trusted-proxy client-IP helper (`X-Real-IP` when the peer is in `TRUSTED_PROXY_CIDRS`; CIDR parsing, a table test of peers and headers), tests with the injected clock | small–medium (the trusted-range setting adds a small helper and one test, no change to the total) |
| 4 | Admin handlers (create, cancel, list, revoke) with errors and tests, incl. cross-tenant 404 | medium |
| 5 | Public `POST /pair/v1/exchange` + limits + tests (single use race, expiry by clock, uniform error, suspended tenant) | medium |
| 6 | `requireToken` device branch (`qdk_`, revoked reason, throttled `last_seen_at`) + tests that old token/tenant code flows are unchanged | small–medium |
| 7 | App: `PairPayload` parser + dispatcher, exchange call in `api.dart`, tests with a fake API | small |
| 8 | App: Settings pairing form, masked key, errors and revoked banner, `l10n.dart` ru/kk, widget tests | medium |
| 9 | README (env/behaviour, section under its own feature) + self-host guide line | small |
| 10 | Front door side (`ai-dala.next`: server route, QR render, device page) | medium — in the site's repository, not here |
| 11 | Option B (section 8) | medium–large — later |
| 12 | Secure storage for secrets on the phone | small–medium — Stage 3, with a new package |

Suggested split into tasks: (A) 1+2+3+4+5+6 server, in two or three pull requests (store + exchange; admin; middleware);
(B) 7+8+9 app; (C) 10 on the site. **Total: about 5–7 developer tasks of medium size on Qoimeks (3–4 server, 2 app) plus
one on the site; none is large, and nothing touches the ledger.**

## 11. Open questions for the owner

1. **Provisioning token on the front door**: is the one that creates tenants acceptable for pairing too (proposed), or do
   you want a second token limited to pairing/devices?
2. **Device limits**: 5 pending codes and 50 devices per tenant, 10-minute default lifetime — right for the plans?
3. **Who may revoke**: only through the front door (admin API), or also from a device in the app/web page (tenant-scoped
   `GET /api/devices`, which needs a "who is allowed" decision since there are no accounts)?
4. ~~**Client IP**~~ **Closed** (infra facts, 2026-10-28; section 4): the client IP is `X-Real-IP` when the direct peer
   is in `TRUSTED_PROXY_CIDRS`, otherwise `RemoteAddr`; the port is bound to `127.0.0.1` and ufw is default-deny.
   Remaining for build time only: infra measures the exact peer address to set the default range.
5. **Per-code counter**: accept the argument in section 4, or have the QR carry an extra public id for an explicit per-code
   attempt counter?
6. **Key storage on the phone**: keep `SharedPreferences` for now (proposed) or add Android secure storage already
   with this feature (new package, pairs with Stage 3 item 4)?
7. **Plain `http` addresses**: a self-hosted server on a local network has no HTTPS; allow pairing to `http` with a warning
   (proposed) or require HTTPS except for loopback?
8. **Expired tenant keys**: should revoked/expired devices be deleted after some months or kept forever?
9. **Client 1**: pair its phones later, or never (the old token stays)?
10. **Option B**: wanted at all, and before or after Google Play?

## Left out on purpose

Per-device roles/permissions, accounts, key rotation by the device itself, QR carrying the device key, pairing from Qoimeks'
own web page, device attestation, Option B code, secure storage on the phone.
