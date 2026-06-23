-- +goose Up
create table students (
  tenant_id   text             not null,
  id          bigint           not null,
  name        text             not null,
  gpa         double precision not null,
  primary key (tenant_id, id)
);

create table users (
  tenant_id     text   not null,
  id            bigint not null,
  email         text   not null unique, -- email -> tenant
  password_hash text   not null,
  primary key (tenant_id, id)
);

create table sessions (
  token       text        not null primary key,
  tenant_id   text        not null,
  user_id     bigint      not null,
  created_at  timestamptz not null,
  expires_at  timestamptz not null
);

-- +goose Down
drop table sessions;
drop table users;
drop table students;
