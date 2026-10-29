-- Pairing codes and per-device keys. Default schema only (see publicOnly in migrate.go):
-- the middleware must find a device and its tenant before it knows the tenant schema,
-- and a tenant schema must not see other tenants' devices. Registry data, not ledger:
-- only the code and key hashes are stored, never the code or the key.
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
    key_hash     bytea not null unique,
    label        text not null,
    operator     text not null default '',
    app_version  text not null default '',
    pairing_id   uuid references device_pairings (pairing_id),
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz,
    revoked_at   timestamptz
);
create index devices_tenant_idx on devices (tenant_id, created_at desc);
