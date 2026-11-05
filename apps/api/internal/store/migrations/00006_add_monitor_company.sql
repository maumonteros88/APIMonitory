-- +goose Up
alter table monitors add column company text;

-- +goose Down
alter table monitors drop column company;
