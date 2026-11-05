-- name: InsertCheck :exec
insert into checks (monitor_id, success, http_status, latency_ms, error_kind, scheduled, status, checked_at)
values ($1, $2, $3, $4, $5, $6, $7, sqlc.arg(checked_at));

-- name: ListScheduledChecksBetween :many
select checked_at, status from checks
where monitor_id = $1 and scheduled
  and checked_at >= sqlc.arg(from_at) and checked_at < sqlc.arg(to_at)
order by checked_at asc;

-- name: ListCheckBuckets :many
-- Aggregates scheduled checks into fixed-size time buckets aligned to the Unix
-- epoch, one row per non-empty (monitor, bucket). Latency is summed over the
-- successful checks together with their count (ok_checks) so the caller can
-- tell "no successful check" from a zero average without nullable aggregates.
-- worst ranks the status: down 3, degraded 2, up 1, anything else (pending or
-- a legacy NULL) 0. The bucket uses integer math on the epoch floored to whole
-- seconds (bucket sizes are whole seconds), so no float rounding at the
-- boundaries. date_bin is avoided on purpose: it needs Postgres 14.
select
  monitor_id,
  to_timestamp((floor(extract(epoch from checked_at))::bigint / sqlc.arg(bucket_seconds)::int::bigint) * sqlc.arg(bucket_seconds)::int::bigint)::timestamptz as bucket,
  count(*)::int as checks,
  count(*) filter (where success and latency_ms is not null)::int as ok_checks,
  coalesce(sum(latency_ms) filter (where success), 0)::bigint as ok_latency_sum_ms,
  coalesce(max(latency_ms) filter (where success), 0)::int as ok_latency_max_ms,
  max(case status when 'down' then 3 when 'degraded' then 2 when 'up' then 1 else 0 end)::int as worst
from checks
where monitor_id = any(sqlc.arg(monitor_ids)::uuid[]) and scheduled
  and checked_at >= sqlc.arg(from_at) and checked_at < sqlc.arg(to_at)
group by monitor_id, bucket
order by monitor_id, bucket;
