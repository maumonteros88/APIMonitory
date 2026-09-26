-- +goose Up
create extension if not exists pgcrypto;

create table monitors (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  description text not null default '',
  url text not null,
  status text not null default 'pending' check (status in ('pending','up','degraded','down')),
  consecutive_failures int not null default 0,
  last_checked_at timestamptz,
  last_http_status int,
  last_latency_ms int,
  last_error_kind text,
  created_at timestamptz not null default now()
);

create table checks (
  id bigserial primary key,
  monitor_id uuid not null references monitors(id) on delete cascade,
  checked_at timestamptz not null default now(),
  success boolean not null,
  http_status int,
  latency_ms int,
  error_kind text,
  scheduled boolean not null
);

create index checks_monitor_checked_at_idx on checks(monitor_id, checked_at desc);

create table daily_stats (
  monitor_id uuid not null references monitors(id) on delete cascade,
  day date not null,
  checks int not null default 0,
  fails int not null default 0,
  down_seconds int not null default 0,
  degraded_seconds int not null default 0,
  primary key (monitor_id, day)
);

-- +goose Down
drop table daily_stats;
drop table checks;
drop table monitors;
