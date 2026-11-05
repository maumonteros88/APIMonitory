-- name: CreateMonitor :one
insert into monitors (name, description, url, service, company, kind, environment)
values ($1, $2, $3, $4, $5, $6, $7)
returning *;

-- name: ListMonitors :many
select * from monitors
order by created_at asc;

-- name: GetMonitor :one
select * from monitors
where id = $1;

-- name: DeleteMonitor :execrows
delete from monitors
where id = $1;

-- name: UpdateMonitorDetails :one
update monitors
set name = $2,
    description = $3,
    service = $4,
    company = $5,
    kind = $6,
    environment = $7
where id = $1
returning *;

-- name: AssignServiceCompany :execrows
update monitors
set company = sqlc.narg('company')
where service = sqlc.arg('service');

-- name: UpdateMonitorAfterCheck :execrows
update monitors
set status = $2,
    consecutive_failures = $3,
    last_checked_at = $4,
    last_http_status = $5,
    last_latency_ms = $6,
    last_error_kind = $7,
    last_commit = $8
where id = $1;
