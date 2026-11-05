// Package store persists monitors, individual checks, and daily aggregates
// in Postgres, running goose migrations on Open and wrapping the generated
// sqlc query layer with the transactional operations the rest of the app
// needs.
package store

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"apimonitory/internal/monitor"
	"apimonitory/internal/store/db"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a requested monitor does not exist. It hides
// the underlying pgx.ErrNoRows so callers (in particular httpapi) don't need
// to depend on pgx to check for it.
var ErrNotFound = errors.New("store: monitor not found")

// Store wraps a Postgres connection pool and the generated query layer.
type Store struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Open connects to Postgres and runs any pending goose migrations before
// returning. Callers must call Close when done.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := migrate(pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, queries: db.New(pool)}, nil
}

func migrate(pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqlDB, "migrations")
}

// Close releases the underlying connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// MonitorLabels are the fields that only organize a monitor on the
// dashboard (grouping by service, filtering by environment). None of them
// changes how the monitor is probed.
type MonitorLabels struct {
	Service     string // "" means no service (stored as NULL)
	Company     string // "" for none (stored as NULL)
	Kind        string // "front" or "api"
	Environment string // "dev", "qa", "prepro", "prod", or "" for none (stored as NULL)
}

// CreateMonitor inserts a new monitor in the pending state.
func (s *Store) CreateMonitor(ctx context.Context, name, description, url string, labels MonitorLabels) (db.Monitor, error) {
	return s.queries.CreateMonitor(ctx, db.CreateMonitorParams{
		Name:        name,
		Description: description,
		Url:         url,
		Service:     optionalText(labels.Service),
		Company:     optionalText(labels.Company),
		Kind:        labels.Kind,
		Environment: optionalText(labels.Environment),
	})
}

// ListMonitors returns all monitors ordered by creation time.
func (s *Store) ListMonitors(ctx context.Context) ([]db.Monitor, error) {
	return s.queries.ListMonitors(ctx)
}

// GetMonitor fetches a monitor by id. Returns ErrNotFound if it doesn't exist.
func (s *Store) GetMonitor(ctx context.Context, id uuid.UUID) (db.Monitor, error) {
	m, err := s.queries.GetMonitor(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Monitor{}, ErrNotFound
	}
	return m, err
}

// UpdateMonitorDetails replaces a monitor's name, description and labels.
// Its URL is intentionally not editable here: changing the probe target is a
// bigger behavioral change than a rename and isn't part of this operation.
// Returns ErrNotFound if no monitor with that id exists.
func (s *Store) UpdateMonitorDetails(ctx context.Context, id uuid.UUID, name, description string, labels MonitorLabels) (db.Monitor, error) {
	m, err := s.queries.UpdateMonitorDetails(ctx, db.UpdateMonitorDetailsParams{
		ID:          id,
		Name:        name,
		Description: description,
		Service:     optionalText(labels.Service),
		Company:     optionalText(labels.Company),
		Kind:        labels.Kind,
		Environment: optionalText(labels.Environment),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Monitor{}, ErrNotFound
	}
	return m, err
}

// AssignServiceCompany sets the company label on every monitor whose service
// is exactly service, and returns how many were updated. An empty company
// clears it (stored as NULL).
func (s *Store) AssignServiceCompany(ctx context.Context, service, company string) (int64, error) {
	return s.queries.AssignServiceCompany(ctx, db.AssignServiceCompanyParams{
		Company: optionalText(company),
		Service: optionalText(service),
	})
}

// DeleteMonitor removes a monitor, cascading its checks and daily_stats.
// Returns ErrNotFound if no monitor with that id existed.
func (s *Store) DeleteMonitor(ctx context.Context, id uuid.UUID) error {
	rows, err := s.queries.DeleteMonitor(ctx, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDailyStatsSince returns the daily_stats rows for a monitor on or after
// since (a date, interpreted in the app's configured timezone by the caller).
func (s *Store) ListDailyStatsSince(ctx context.Context, id uuid.UUID, since time.Time) ([]db.DailyStat, error) {
	return s.queries.ListDailyStatsSince(ctx, db.ListDailyStatsSinceParams{
		MonitorID: id,
		Day:       pgtype.Date{Time: dayOf(since), Valid: true},
	})
}

// ListAllDailyStatsSince returns the daily_stats rows for every monitor, on
// or after since, in one query. Used by the list endpoint to avoid one
// ListDailyStatsSince call per monitor.
func (s *Store) ListAllDailyStatsSince(ctx context.Context, since time.Time) ([]db.DailyStat, error) {
	return s.queries.ListAllDailyStatsSince(ctx, pgtype.Date{Time: dayOf(since), Valid: true})
}

// ListScheduledChecksBetween returns the scheduled checks rows for a monitor
// in [from, to), ascending by checked_at. Manual checks are excluded: they
// don't count toward daily_stats, so they shouldn't surface as incidents
// either (they'd inflate minutes the tooltip never showed).
func (s *Store) ListScheduledChecksBetween(ctx context.Context, id uuid.UUID, from, to time.Time) ([]db.ListScheduledChecksBetweenRow, error) {
	return s.queries.ListScheduledChecksBetween(ctx, db.ListScheduledChecksBetweenParams{
		MonitorID: id,
		FromAt:    pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:      pgtype.Timestamptz{Time: to, Valid: true},
	})
}

// ListCheckBuckets aggregates the scheduled checks of the given monitors in
// [from, to) into buckets of bucketSeconds, aligned to the Unix epoch, in one
// query. Only non-empty buckets come back; the caller fills the gaps. Manual
// checks are excluded for the same reason as in ListScheduledChecksBetween.
func (s *Store) ListCheckBuckets(ctx context.Context, ids []uuid.UUID, from, to time.Time, bucketSeconds int) ([]db.ListCheckBucketsRow, error) {
	return s.queries.ListCheckBuckets(ctx, db.ListCheckBucketsParams{
		BucketSeconds: int32(bucketSeconds),
		MonitorIds:    ids,
		FromAt:        pgtype.Timestamptz{Time: from, Valid: true},
		ToAt:          pgtype.Timestamptz{Time: to, Valid: true},
	})
}

// Attempt describes one completed probe attempt to persist.
type Attempt struct {
	MonitorID     uuid.UUID
	Result        monitor.ProbeResult
	NewStatus     monitor.Status
	NewFailures   int
	Scheduled     bool          // whether this attempt came from the periodic scheduler (vs. a manual check)
	CheckInterval time.Duration // added to down_seconds/degraded_seconds when Scheduled
	Now           time.Time     // probe completion time, in the app's configured timezone
}

// RegisterAttempt persists the outcome of one probe attempt in a single
// transaction: it updates the monitor's cached state, appends a checks row,
// and — for scheduled attempts only — upserts today's daily_stats row,
// adding CheckInterval seconds to down_seconds or degraded_seconds depending
// on the attempt's final status.
//
// If the monitor was deleted concurrently (the UPDATE affects 0 rows), the
// attempt is logged and dropped: there is nothing left to attach the
// checks/daily_stats rows to, and this is not treated as a fatal error.
func (s *Store) RegisterAttempt(ctx context.Context, a Attempt) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op once Commit has succeeded

	q := s.queries.WithTx(tx)

	rows, err := q.UpdateMonitorAfterCheck(ctx, db.UpdateMonitorAfterCheckParams{
		ID:                  a.MonitorID,
		Status:              string(a.NewStatus),
		ConsecutiveFailures: int32(a.NewFailures),
		LastCheckedAt:       pgtype.Timestamptz{Time: a.Now, Valid: true},
		LastHttpStatus:      optionalInt4(a.Result.HTTPStatus),
		LastLatencyMs:       validInt4(a.Result.LatencyMs),
		LastErrorKind:       optionalText(a.Result.ErrorKind),
		LastCommit:          optionalText(a.Result.Commit),
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		slog.Warn("dropping check attempt: monitor no longer exists", "monitor_id", a.MonitorID)
		return nil
	}

	if err := q.InsertCheck(ctx, db.InsertCheckParams{
		MonitorID:  a.MonitorID,
		Success:    a.Result.Success,
		HttpStatus: optionalInt4(a.Result.HTTPStatus),
		LatencyMs:  validInt4(a.Result.LatencyMs),
		ErrorKind:  optionalText(a.Result.ErrorKind),
		Scheduled:  a.Scheduled,
		Status:     pgtype.Text{String: string(a.NewStatus), Valid: true},
		CheckedAt:  pgtype.Timestamptz{Time: a.Now, Valid: true},
	}); err != nil {
		return err
	}

	if a.Scheduled {
		var fails, downSeconds, degradedSeconds int32
		if !a.Result.Success {
			fails = 1
		}
		intervalSeconds := int32(a.CheckInterval.Seconds())
		switch a.NewStatus {
		case monitor.StatusDown:
			downSeconds = intervalSeconds
		case monitor.StatusDegraded:
			degradedSeconds = intervalSeconds
		}

		if err := q.UpsertDailyStats(ctx, db.UpsertDailyStatsParams{
			MonitorID:       a.MonitorID,
			Day:             pgtype.Date{Time: dayOf(a.Now), Valid: true},
			Fails:           fails,
			DownSeconds:     downSeconds,
			DegradedSeconds: degradedSeconds,
		}); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// dayOf strips the time-of-day component, keeping the Y-M-D as observed in
// t's own location, and re-anchors it in UTC so pgtype.Date (a date-only
// wire type) never shifts by a day due to timezone reinterpretation.
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// optionalInt4 maps 0 to SQL NULL: used for fields where 0 means "no value"
// (e.g. an HTTP status that was never received).
func optionalInt4(v int) pgtype.Int4 {
	if v == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(v), Valid: true}
}

// validInt4 always stores v: used for fields that are always measured
// (latency is timed around every probe, success or failure).
func validInt4(v int) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(v), Valid: true}
}

func optionalText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
