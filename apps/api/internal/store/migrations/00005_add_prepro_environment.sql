-- +goose Up
alter table monitors drop constraint monitors_environment_check;
alter table monitors add constraint monitors_environment_check
  check (environment in ('dev','qa','prepro','prod'));

-- +goose Down
-- The old check rejects 'prepro', so those monitors lose their environment
-- (they show up as unassigned) instead of blocking the rollback.
update monitors set environment = null where environment = 'prepro';
alter table monitors drop constraint monitors_environment_check;
alter table monitors add constraint monitors_environment_check
  check (environment in ('dev','qa','prod'));
