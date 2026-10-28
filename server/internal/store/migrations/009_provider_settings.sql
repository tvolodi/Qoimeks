-- Server-wide language-model and speech provider settings (default schema only,
-- see publicOnly in migrate.go). One row per kind. The API key is encrypted by the
-- application; this is configuration, not ledger, so rows are updated in place.
create table provider_settings (
    kind         text primary key check (kind in ('llm', 'stt')),
    provider     text not null,
    model        text not null default '',
    api_key_enc  bytea,
    key_hint     text not null default '',
    updated_at   timestamptz not null default now()
);
