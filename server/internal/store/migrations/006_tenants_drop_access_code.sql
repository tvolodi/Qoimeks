-- Only the hash of a tenant's access code is kept (access_hash); the clear text goes.
alter table tenants drop column access_code;
