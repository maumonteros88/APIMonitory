-- name: InsertCheck :exec
insert into checks (monitor_id, success, http_status, latency_ms, error_kind, scheduled, status, checked_at)
values ($1, $2, $3, $4, $5, $6, $7, sqlc.arg(checked_at));

-- name: ListScheduledChecksBetween :many
select checked_at, status from checks
where monitor_id = $1 and scheduled
  and checked_at >= sqlc.arg(from_at) and checked_at < sqlc.arg(to_at)
order by checked_at asc;
