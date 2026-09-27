-- name: CreateMonitor :one
insert into monitors (name, description, url)
values ($1, $2, $3)
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
    description = $3
where id = $1
returning *;

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
