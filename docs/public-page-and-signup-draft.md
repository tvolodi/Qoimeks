# Qoimeks on ai-dala.com: public page and registration (DRAFT)

Status (2026-10-22): the owner approved the tenant model (one PostgreSQL schema per tenant)
and chose section 3 as the starting contract; problems are to be solved as they appear.
Nothing in section 3 or 4 is built yet. Sources: `project/goal.md` (v3 "tiers and tenants"), README.

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

## 3. Proposed contract (OpenAPI-style sketch; Qoimeks owns it)

Base: `https://qoimeks.ai-dala.com/admin/v1`. Server-to-server only; the browser never calls it.
Auth: `Authorization: Bearer <PROVISIONING_TOKEN>` (separate from the app's `ACCESS_TOKEN`).
Rate limit: 30 req/min per token (the site is the only caller). Idempotent by `tenant_id`.

```
POST /tenants
  { "tenant_id": "uuid", "name": "string", "language": "ru|kk|en",
    "tier": "demo|hosted_own_keys|hosted_all_inclusive",
    "owner_email": "string" }
  201 { "tenant_id", "status": "active", "server_url": "https://...",
        "access_code": "string" }      // shown once; the site gives it to the customer
  200 same body if tenant_id already exists (idempotent)
  400 invalid_request | 401 unauthorized | 429 rate_limited

POST /tenants/{tenant_id}/suspend   -> 200 { "status": "suspended" }
POST /tenants/{tenant_id}/resume    -> 200 { "status": "active" }
GET  /tenants/{tenant_id}           -> 200 { "tenant_id", "status", "tier", "created_at" }
404 for unknown tenant; errors: { "error": "code", "message": "string" }
```

Open questions (to settle as they appear): how a tenant gets its own URL/subdomain vs a path; whether
`access_code` per tenant replaces the shared `ACCESS_TOKEN`; payment-to-tier mapping.

## 4. What is available for QA now

- Nothing live. The tenant API does not exist, and building it is not queued yet.
- You can code against section 3 with a stub. Suggested stub behaviour: `POST /tenants`
  returns 201 with `server_url: "https://qoimeks.ai-dala.com"` and a fake `access_code`;
  repeat call returns 200; `suspend`/`resume` flip the status.
- A real, existing thing to link to: the static download page above, and (if the owner
  agrees) the v2 demo server once built.

Remaining step: queue the tenant API work in the team (`team.py add`).
