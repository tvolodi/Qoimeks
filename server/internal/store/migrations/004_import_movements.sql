-- Which import posted a movement, so everything one workbook changed can be found
-- and cancelled together. Deferred: the import's own row is written last, with its totals.
alter table movements
    add column import_id bigint references imports deferrable initially deferred;

create index movements_import_idx on movements (import_id) where import_id is not null;
