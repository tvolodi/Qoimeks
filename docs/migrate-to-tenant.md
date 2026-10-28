# Copying the default schema into a tenant

`server/cmd/migrate-tenant` copies, never moves, the ledger of the default schema
(`public`) into the schema of an existing tenant. It is step 2 of the Client 1 migration
(D-042). Using it in production is a gated release: the product owner runs it.

## What it copies

`locations`, `skus`, `imports`, `movements`, `balances` and `feedback` (every table the
migrations 001–004 and 008 create in a tenant schema), in one transaction on a consistent
snapshot. Ids, timestamps and the positions of the identity sequences are kept, so the
next insert in the tenant gets a free id. The source is not changed. The registry
(`tenants`) and `provider_settings` are not copied.

## Checks (all in the same transaction)

- The tenant exists, the source schema exists, and the table columns of both match
  (otherwise start the server once so it migrates the tenant, then retry).
- Every target table is empty. Otherwise the tool refuses; this makes a second run fail
  clearly instead of duplicating data.
- After the copy: row count and an md5 over the ordered rows per table, and an md5 of the
  current balance per SKU and location, are equal in source and target. Any difference
  aborts and rolls everything back.

The report on stdout holds counts, md5 sums and the time per table. It never prints row
contents, tokens or `DATABASE_URL`.

## Usage

```
cd server
DATABASE_URL=... go run ./cmd/migrate-tenant --tenant-id <tenant uuid> --dry-run
DATABASE_URL=... go run ./cmd/migrate-tenant --tenant-id <tenant uuid>
```

In production, use the tool shipped in the server image
(`/usr/local/bin/qoimeks-migrate-tenant`); no Go toolchain is needed on the host. The
running container already has `DATABASE_URL`, and the tool never prints it:

```
docker exec <container> qoimeks-migrate-tenant --tenant-id <tenant uuid> --dry-run
docker exec <container> qoimeks-migrate-tenant --tenant-id <tenant uuid>
```

Without a running container, `docker run --rm -e DATABASE_URL=... --entrypoint
qoimeks-migrate-tenant <image> --tenant-id <tenant uuid> --dry-run` does the same.

Flags: `--tenant-id` (required), `--source-schema` (default `public`), `--dry-run`
(does everything, rolls back, prints the report; sequences are not touched).

Exit code 0: success. 1: refused or failed, nothing was changed.

## Procedure and rollback

1. Freeze writes: restart the server with `MAINTENANCE_MODE=1` so nothing changes during the
   copy (writes get 503, reads and `/healthz` keep working); unfreeze after the check.
2. Dump before anything else, and keep the file outside the server:
   `pg_dump --format=custom --file=qoimeks-before-copy.dump "$DATABASE_URL"`.
3. Create the tenant (admin API `POST /tenants`) if it does not exist yet.
4. Run with `--dry-run`, read the report.
5. Run for real. The run is one transaction: if it fails, the tenant is as it was.
6. Rollback after a successful run (before the tenant is used): the copy is additive, the
   source is intact, so switching back means pointing clients at the shared access token
   again. To empty the tenant's tables, the append-only triggers forbid DELETE and
   TRUNCATE; drop and recreate the tenant schema (`drop schema t_<id> cascade`, then
   create the tenant again) or restore the dump with `pg_restore`.
