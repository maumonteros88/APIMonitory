-- name: InsertCheck :exec
insert into checks (monitor_id, success, http_status, latency_ms, error_kind, scheduled)
values ($1, $2, $3, $4, $5, $6);
