package httpapi

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"apimonitory/internal/store/db"
)

// latencyRange is one of the windows GET /api/monitors/latency accepts. The
// bucket size is chosen so every range yields a similar, chart-friendly
// number of points (60 to 96).
type latencyRange struct {
	name    string
	bucket  time.Duration
	buckets int
}

var latencyRanges = map[string]latencyRange{
	"24h": {name: "24h", bucket: 15 * time.Minute, buckets: 96},
	"7d":  {name: "7d", bucket: 2 * time.Hour, buckets: 84},
	"30d": {name: "30d", bucket: 12 * time.Hour, buckets: 60},
}

type latencyResponse struct {
	Range         string           `json:"range"`
	BucketMinutes int              `json:"bucketMinutes"`
	From          string           `json:"from"`
	To            string           `json:"to"`
	Monitors      []monitorLatency `json:"monitors"`
}

type monitorLatency struct {
	ID     string         `json:"id"`
	Points []latencyPoint `json:"points"`
}

// latencyPoint is one bucket. AvgMs and MaxMs are null when the bucket has
// no successful check, since a failed probe's latency (often a timeout) is
// not a response time.
type latencyPoint struct {
	At     string `json:"at"`
	AvgMs  *int   `json:"avgMs"`
	MaxMs  *int   `json:"maxMs"`
	Checks int    `json:"checks"`
	Status string `json:"status"`
}

// latencyStatuses maps the rank ListCheckBuckets returns in "worst" back to
// a status name; index 0 covers pending and legacy NULL-status checks.
var latencyStatuses = [...]string{"none", "up", "degraded", "down"}

// handleLatency returns latency and status aggregated into time buckets for
// the chart view. Aggregation happens in SQL (a 30 day window holds about a
// million check rows); Go only fills in the empty buckets.
func (a *api) handleLatency(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = "24h"
	}
	rng, ok := latencyRanges[rangeName]
	if !ok {
		writeError(w, http.StatusBadRequest, "El parámetro range debe ser 24h, 7d o 30d")
		return
	}

	var wanted map[uuid.UUID]bool // nil means every monitor
	if raw := r.URL.Query().Get("ids"); raw != "" {
		wanted = make(map[uuid.UUID]bool)
		for _, part := range strings.Split(raw, ",") {
			id, err := uuid.Parse(strings.TrimSpace(part))
			if err != nil {
				writeError(w, http.StatusBadRequest, "El parámetro ids tiene un id inválido")
				return
			}
			wanted[id] = true
		}
	}

	// Listing the monitors both resolves "every monitor" and drops unknown
	// ids, without one lookup per id.
	monitors, err := a.store.ListMonitors(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(monitors))
	for _, m := range monitors {
		if wanted == nil || wanted[m.ID] {
			ids = append(ids, m.ID)
		}
	}

	from, to := latencyWindow(a.now(), rng)
	resp := latencyResponse{
		Range:         rng.name,
		BucketMinutes: int(rng.bucket / time.Minute),
		From:          from.Format(time.RFC3339),
		To:            to.Format(time.RFC3339),
		Monitors:      make([]monitorLatency, 0, len(ids)),
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	rows, err := a.store.ListCheckBuckets(r.Context(), ids, from, to, int(rng.bucket/time.Second))
	if err != nil {
		writeInternalError(w, err)
		return
	}

	index := make(map[uuid.UUID]int, len(ids))
	for i, id := range ids {
		index[id] = i
		resp.Monitors = append(resp.Monitors, monitorLatency{ID: id.String(), Points: emptyLatencyPoints(from, rng)})
	}
	for _, row := range rows {
		i, known := index[row.MonitorID]
		slot := int(row.Bucket.Time.Sub(from) / rng.bucket)
		if !known || row.Bucket.Time.Before(from) || slot >= rng.buckets {
			continue
		}
		resp.Monitors[i].Points[slot] = latencyPointFromRow(row)
	}

	writeJSON(w, http.StatusOK, resp)
}

// latencyWindow returns [from, to) for rng: buckets aligned to multiples of
// the bucket size since the Unix epoch, the last one being the bucket that
// contains now. Aligning to the epoch (not to now) keeps bucket boundaries
// stable between polls, so a point only changes while its bucket is open.
func latencyWindow(now time.Time, rng latencyRange) (from, to time.Time) {
	size := int64(rng.bucket / time.Second)
	last := now.Unix() / size * size
	from = time.Unix(last-int64(rng.buckets-1)*size, 0).UTC()
	to = time.Unix(last+size, 0).UTC()
	return from, to
}

func emptyLatencyPoints(from time.Time, rng latencyRange) []latencyPoint {
	points := make([]latencyPoint, rng.buckets)
	for i := range points {
		points[i] = latencyPoint{At: from.Add(time.Duration(i) * rng.bucket).Format(time.RFC3339), Status: "none"}
	}
	return points
}

func latencyPointFromRow(row db.ListCheckBucketsRow) latencyPoint {
	p := latencyPoint{
		At:     row.Bucket.Time.UTC().Format(time.RFC3339),
		Checks: int(row.Checks),
		Status: latencyStatuses[0],
	}
	if row.Worst > 0 && int(row.Worst) < len(latencyStatuses) {
		p.Status = latencyStatuses[row.Worst]
	}
	if row.OkChecks > 0 {
		avg := int(math.Round(float64(row.OkLatencySumMs) / float64(row.OkChecks)))
		max := int(row.OkLatencyMaxMs)
		p.AvgMs = &avg
		p.MaxMs = &max
	}
	return p
}
