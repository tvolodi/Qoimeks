create table skus (
    id      bigint generated always as identity primary key,
    code    text not null unique,
    name    text not null,
    unit    text not null default 'шт',
    barcode text,
    active  boolean not null default true
);

create table locations (
    id     bigint generated always as identity primary key,
    code   text not null unique,
    name   text not null default '',
    active boolean not null default true
);

-- One row per operation an operator performed. Never updated or deleted:
-- a mistake is corrected by a new row whose "reverses" points at the wrong one.
create table movements (
    id             uuid primary key, -- generated on the phone, makes retries idempotent
    seq            bigint generated always as identity unique,
    kind           text not null check (kind in ('receipt', 'issue', 'transfer', 'adjustment')),
    sku_id         bigint not null references skus,
    location_id    bigint not null references locations,
    to_location_id bigint references locations,
    -- receipt/issue/transfer: quantity moved; adjustment: quantity counted
    qty            numeric(14, 3) not null check (qty >= 0),
    operator       text not null check (operator <> ''),
    source         text not null check (source in ('keyboard', 'voice')),
    transcript     text,
    reverses       uuid unique references movements,
    created_at     timestamptz not null default now(),
    check ((kind = 'transfer') = (to_location_id is not null)),
    check (to_location_id is null or to_location_id <> location_id),
    check (kind = 'adjustment' or qty > 0)
);

-- One row per (movement, affected location): the change and the balance after it.
-- The current balance of a SKU at a location is its latest row here.
create table balances (
    id          bigint generated always as identity primary key,
    movement_id uuid not null references movements,
    sku_id      bigint not null references skus,
    location_id bigint not null references locations,
    delta       numeric(14, 3) not null,
    qty         numeric(14, 3) not null,
    created_at  timestamptz not null default now()
);

create index balances_current_idx on balances (sku_id, location_id, id desc);
create index balances_movement_idx on balances (movement_id);

create function forbid_change() returns trigger language plpgsql as $$
begin
    raise exception '% is append-only', tg_table_name;
end $$;

create trigger movements_append_only before update or delete or truncate on movements
    for each statement execute function forbid_change();
create trigger balances_append_only before update or delete or truncate on balances
    for each statement execute function forbid_change();
