-- +goose Up
alter table monitors add column last_commit text;

-- +goose Down
alter table monitors drop column last_commit;
