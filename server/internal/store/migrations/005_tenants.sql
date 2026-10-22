-- Registry of tenants. Default schema only (see publicOnly in migrate.go): a tenant
-- schema holds the ledger tables and must not see this table.
create table tenants (
    tenant_id   uuid primary key,
    name        text not null,
    language    text not null,
    tier        text not null,
    owner_email text not null default '',
    schema_name text not null unique,
    access_hash bytea not null unique,
    access_code text not null,
    status      text not null default 'active' check (status in ('active', 'suspended')),
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);
