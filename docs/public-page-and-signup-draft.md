# Qoimeks on ai-dala.com: public page and registration (DRAFT)

Status (2026-10-22): the owner approved the tenant model (one PostgreSQL schema per tenant)
and chose section 3 as the starting contract. The tenant API (section 3) is built in the
Qoimeks server (task T-0010); section 3 below is the contract as implemented. Sources: `project/goal.md` (v3 "tiers and tenants"), README.

## 1. What Qoimeks is (text for the public page)

**One line.** Stock accounting by SKU and storage location: say the operation aloud or fill
one form, check the draft, post it.

**Audience.** Warehouse operators on Android phones (often gloves, noise, non-technical);
bookkeepers and the warehouse manager in a browser; owners of small warehouses/shops who keep
stock in Excel. Later: people who want a "where is my thing" home profile.

**Key features (true today, proof of concept, one warehouse in the field)**
- One form for receipt, issue, transfer and stock count; by hand or by voice.
- Voice input in Russian and Kazakh: speech-to-text, then a language model turns the phrase
  into a draft the operator checks before posting.
- Append-only ledger on PostgreSQL; posting is idempotent.
- Browser interface: balances, history, Excel import with check and revert (the bookkeepers'
  weekly workbook stays the authority), Excel export.
- Languages of the product: ru, kk (app and download page); en is README-level only today,
  so the public page can be ru/kk/en but the app UI itself is not yet in English.

**Planned, do not present as available:** in-app feedback, offline queue, standalone mode with
no server, Google Play release, hosted tiers (below).

**Free / self-host:** the repo is public (github.com/tvolodi/Qoimeks) under AGPL-3.0. The
"self-host in 15 minutes" guide is not written yet; the README has run instructions only.

**Screenshots:** none are prepared. I can't produce them from this session without the owner
approving a capture task; ask the owner, or use placeholders for QA.

**Download page that already exists:** `https://ai-dala.com/qoimeks/` (static, nginx, ru/kk,
APK + `latest.json`). The new description page should link to it, not replace it.

## 2. Multitenant plan (direction, not a decision)

- Tiers: free self-hosted (own keys); paid hosting with own keys; paid all-inclusive with the
  owner's keys. Plus a v2 demo server with a small per-device quota of AI calls.
- Tenant model (approved): one PostgreSQL schema per tenant so ledger, triggers and
  queries stay unchanged. Per-tenant metering of AI usage on top of the demo quota.
- Division of labour already written in goal.md: **CRM, payments, sign-in and tenant
  provisioning live in ai-dala.next**; Qoimeks exposes only a small create/suspend tenant API.
  So the site should NOT expect Qoimeks to own accounts, passwords or email verification.
- Registration should collect (the site's side): organisation name, contact name, email
  (verified by the site), preferred language (ru/kk/en), tier, consent to terms. Qoimeks only
  needs: `tenant_id` (site-generated), display name, language, tier, and for the
  all-inclusive/own-keys distinction a flag; own keys are entered later in the Qoimeks web admin
  (stage 3), never passed through the site.

## 3. Contract (implemented; Qoimeks owns it)

Base: `https://qoimeks.ai-dala.com/admin/v1` (same origin as the app API). Server-to-server
only; the browser never calls it.
Auth: `Authorization: Bearer <PROVISIONING_TOKEN>` (server env variable, separate from the
app's `ACCESS_TOKEN`). Without a valid token every route answers 401 `unauthorized`. When the
server has no `PROVISIONING_TOKEN` the whole API is off and answers 404.
Rate limit: 30 requests per rolling minute for the token (the site is the only caller); over it
429 `rate_limited` with a `Retry-After` header. Calls with a wrong token are not counted.
Idempotent by `tenant_id`.

```
POST /tenants
  { "tenant_id": "uuid", "name": "string (1..200)", "language": "ru|kk|en",
    "tier": "demo|production", "plan": "own_keys|all_inclusive (optional)",
    "hostname": "string (optional, accepted and ignored)",
    "owner_email": "string (<=254, optional)" }
  201 { "tenant_id", "status": "active", "server_url": "https://...",
        "access_code": "string" }
  200 { "tenant_id", "status", "server_url" } if tenant_id already exists (idempotent);
      NO access_code in the body
  400 invalid_request | 401 unauthorized | 429 rate_limited

POST /tenants/{tenant_id}/rotate-code
  200 { "tenant_id", "access_code": "string" }   (new code, shown once; the old one stops
      working at once; not idempotent: every call rotates; allowed for suspended tenants)
  401 unauthorized | 404 unknown tenant | 429 rate_limited

POST /tenants/{tenant_id}/suspend   -> 200 { "status": "suspended" }   (idempotent)
POST /tenants/{tenant_id}/resume    -> 200 { "status": "active" }      (idempotent)
GET  /tenants/{tenant_id}           -> 200 { "tenant_id", "status", "tier": "demo|production",
                                            "plan": "own_keys|all_inclusive|null",
                                            "stored_tier", "created_at" }
404 for unknown tenant; errors: { "error": "code", "message": "string" }
```

Behaviour that is not obvious from the shapes:
- `server_url` is the server's public base URL (`PUBLIC_URL`, default
  `https://qoimeks.ai-dala.com`). All tenants share it; there is no per-tenant subdomain or path.
- `access_code` is the tenant's own code. The app and the web interface send it exactly like
  `ACCESS_TOKEN` (`Authorization: Bearer <code>`) and are served from the tenant's own schema.
  The shared `ACCESS_TOKEN` keeps serving the default schema, so single-tenant installs are
  unchanged. The code is shown once: the server keeps only its hash, so it is returned only
  by the 201 of the first POST and by `rotate-code`. A repeated POST (200) returns no code; the
  site must store the code at once (treat it like a password). A lost code is replaced with
  `rotate-code`, which gives a new one and invalidates the old.
- A repeated POST returns the stored tenant and ignores changed `name`, `tier` or other fields;
  there is no update endpoint yet. Two simultaneous first POSTs give one 201 and one 200.
- A suspended tenant gets 403 `{"error": "tenant suspended"}` on every `/api/*` call; its data
  is untouched and `resume` restores access at once.
- Tier and plan are stored as one of three values: `demo` -> `demo`; `production` + `own_keys`
  (or no plan) -> `hosted_own_keys`; `production` + `all_inclusive` -> `hosted_all_inclusive`.
  `plan` is ignored with `tier=demo` and defaults to `own_keys` with `production`; an invalid
  `plan` with `production`, or an unknown `tier`, gives 400 `invalid_request`. The three stored
  values are still accepted as `tier` (legacy); `plan` is then ignored. `GET` returns `tier` as
  `demo|production`, `plan` as `own_keys|all_inclusive` (null for demo) and the stored value as
  `stored_tier`.
- `hostname` is accepted and ignored, not stored: all tenants share one origin. Other unknown
  fields are tolerated too.
- `name`, `language`, `tier` and `owner_email` are stored but do not change behaviour yet.

Settled questions: a tenant has no URL of its own (one shared origin, selected by access code);
the per-tenant `access_code` is added next to the shared `ACCESS_TOKEN`, it does not replace it.
Still open: payment-to-tier mapping (the site's side).

## 4. What is available for QA now

- The tenant API of section 3 is built. It goes live when the owner deploys the server with
  `PROVISIONING_TOKEN` (and optionally `PUBLIC_URL`) set in the server's `.env`; until then
  `/admin/v1` answers 404 on production.
- Until the deploy you can still code against section 3 with a stub: `POST /tenants` returns 201
  with `server_url: "https://qoimeks.ai-dala.com"` and a fake `access_code`; a repeat returns 200 without `access_code`; `rotate-code` returns a new fake code;
  `suspend`/`resume` flip the status.
- A real, existing thing to link to: the static download page above, and (if the owner
  agrees) the v2 demo server once built.
