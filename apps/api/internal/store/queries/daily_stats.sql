-- name: UpsertDailyStats :exec
insert into daily_stats (monitor_id, day, checks, fails, down_seconds, degraded_seconds)
values ($1, $2, 1, $3, $4, $5)
on conflict (monitor_id, day) do update
set checks = daily_stats.checks + 1,
    fails = daily_stats.fails + excluded.fails,
    down_seconds = daily_stats.down_seconds + excluded.down_seconds,
    degraded_seconds = daily_stats.degraded_seconds + excluded.degraded_seconds;

-- name: ListDailyStatsSince :many
select * from daily_stats
where monitor_id = $1 and day >= $2
order by day asc;
