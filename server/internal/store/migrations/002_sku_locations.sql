-- Where a SKU normally lives, and a second place to look when that one is full.
alter table skus
    add column location_id bigint references locations,
    add column alt_location_id bigint references locations;

-- Opening balances loaded from the bookkeepers' Excel file.
alter table movements drop constraint movements_source_check;
alter table movements add constraint movements_source_check check (source in ('keyboard', 'voice', 'import'));
