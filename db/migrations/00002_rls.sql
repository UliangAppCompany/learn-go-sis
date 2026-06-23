-- +goose Up
-- Tenant isolation enforced by the database. FORCE so the policy binds even the
-- table owner; with a dedicated non-owning runtime role you could drop FORCE.
alter table students enable row level security;
alter table students force row level security;
create policy tenant_isolation on students
  using (tenant_id = current_setting('app.tenant_id', true))
  with check (tenant_id = current_setting('app.tenant_id', true));

-- +goose Down
drop policy if exists tenant_isolation on students;
alter table students disable row level security;
