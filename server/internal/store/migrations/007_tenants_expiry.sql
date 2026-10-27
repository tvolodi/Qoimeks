-- Demo tenants expire. expires_at is null for tenants that never do; status 'expired'
-- is set by the expire call. Default schema only (see publicOnly in migrate.go).
alter table tenants add column expires_at timestamptz;
alter table tenants drop constraint tenants_status_check;
alter table tenants add constraint tenants_status_check check (status in ('active', 'suspended', 'expired'));
