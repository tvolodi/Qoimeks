-- Who loaded which workbook, and what it changed. The workbook itself is never stored.
create table imports (
    id                bigint generated always as identity primary key,
    created_at        timestamptz not null default now(),
    who               text not null,
    filename          text not null,
    sheet             text not null,
    sha256            text not null,
    with_balances     boolean not null,
    skus_created      int not null,
    skus_updated      int not null,
    locations_created int not null,
    balances_posted   int not null,
    remarks           int not null
);

create trigger imports_append_only before update or delete or truncate on imports
    for each statement execute function forbid_change();
