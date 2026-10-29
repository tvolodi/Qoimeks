-- Codes (barcodes, QR, typed or reader-entered tags) that point at one SKU or location.
-- Catalog data, not ledger: rows are only ever soft-removed (removed_at). Runs in the
-- default schema and in every tenant schema, so the tenant scope needs no tenant_id.
create table codes (
    id          uuid primary key,
    object_type text not null check (object_type in ('sku', 'location')),
    object_id   bigint not null,
    code_type   text not null check (code_type in ('ean13', 'ean8', 'code128', 'qr', 'manual')),
    code        text not null check (code <> '' and code = btrim(code)),
    created_by  text not null check (created_by <> ''),
    created_at  timestamptz not null default now(),
    removed_at  timestamptz,
    removed_by  text
);
-- One code points to exactly one object per tenant; removed rows free the code.
create unique index codes_code_active_uq on codes (code) where removed_at is null;
create index codes_object_idx on codes (object_type, object_id) where removed_at is null;

-- Copy the existing skus.barcode values. Two SKUs with the same barcode: the lowest id keeps it.
insert into codes (id, object_type, object_id, code_type, code, created_by)
select gen_random_uuid(), 'sku', id,
       case
         when code ~ '^[0-9]{13}$' and (10 - (
              select sum(substr(code, i, 1)::int * case when i % 2 = 1 then 1 else 3 end)
              from generate_series(1, 12) i) % 10) % 10 = substr(code, 13, 1)::int then 'ean13'
         when code ~ '^[0-9]{8}$' and (10 - (
              select sum(substr(code, i, 1)::int * case when i % 2 = 1 then 3 else 1 end)
              from generate_series(1, 7) i) % 10) % 10 = substr(code, 8, 1)::int then 'ean8'
         else 'manual'
       end,
       code, 'migration'
from (
    select distinct on (btrim(barcode)) id, btrim(barcode) as code
    from skus
    where barcode is not null and btrim(barcode) <> ''
    order by btrim(barcode), id
) b;
