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
    "tier": "demo|hosted_own_keys|hosted_all_inclusive",
    "owner_email": "string (<=254, optional)" }
  201 { "tenant_id", "status": "active", "server_url": "https://...",
        "access_code": "string" }
  200 same body if tenant_id already exists (idempotent)
  400 invalid_request | 401 unauthorized | 429 rate_limited

POST /tenants/{tenant_id}/suspend   -> 200 { "status": "suspended" }   (idempotent)
POST /tenants/{tenant_id}/resume    -> 200 { "status": "active" }      (idempotent)
GET  /tenants/{tenant_id}           -> 200 { "tenant_id", "status", "tier", "created_at" }
404 for unknown tenant; errors: { "error": "code", "message": "string" }
```

Behaviour that is not obvious from the shapes:
- `server_url` is the server's public base URL (`PUBLIC_URL`, default
  `https://qoimeks.ai-dala.com`). All tenants share it; there is no per-tenant subdomain or path.
- `access_code` is the tenant's own code. The app and the web interface send it exactly like
  `ACCESS_TOKEN` (`Authorization: Bearer <code>`) and are served from the tenant's own schema.
  The shared `ACCESS_TOKEN` keeps serving the default schema, so single-tenant installs are
  unchanged. A repeated POST returns the same stored code, so the site may ask again, but it
  should keep the code (treat it like a password).
- A repeated POST returns the stored tenant and ignores changed `name`, `tier` or other fields;
  there is no update endpoint yet. Two simultaneous first POSTs give one 201 and one 200.
- A suspended tenant gets 403 `{"error": "tenant suspended"}` on every `/api/*` call; its data
  is untouched and `resume` restores access at once.
- `name`, `language`, `tier` and `owner_email` are stored but do not change behaviour yet.

Settled questions: a tenant has no URL of its own (one shared origin, selected by access code);
the per-tenant `access_code` is added next to the shared `ACCESS_TOKEN`, it does not replace it.
Still open: payment-to-tier mapping (the site's side).

## 4. What is available for QA now

- The tenant API of section 3 is built. It goes live when the owner deploys the server with
  `PROVISIONING_TOKEN` (and optionally `PUBLIC_URL`) set in the server's `.env`; until then
  `/admin/v1` answers 404 on production.
- Until the deploy you can still code against section 3 with a stub: `POST /tenants` returns 201
  with `server_url: "https://qoimeks.ai-dala.com"` and a fake `access_code`; a repeat returns 200;
  `suspend`/`resume` flip the status.
- A real, existing thing to link to: the static download page above, and (if the owner
  agrees) the v2 demo server once built.
