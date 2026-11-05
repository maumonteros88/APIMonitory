-- +goose Up
alter table monitors add column service text;
alter table monitors add column kind text not null default 'api';
alter table monitors add constraint monitors_kind_check
  check (kind in ('front','api'));
alter table monitors add column environment text;
alter table monitors add constraint monitors_environment_check
  check (environment in ('dev','qa','prod'));

-- +goose Down
alter table monitors drop column environment;
alter table monitors drop column kind;
alter table monitors drop column service;
