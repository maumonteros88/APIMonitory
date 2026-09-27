-- +goose Up
alter table checks add column status text;
alter table checks add constraint checks_status_check
  check (status in ('pending','up','degraded','down')) not valid;

-- +goose Down
alter table checks drop column status;
