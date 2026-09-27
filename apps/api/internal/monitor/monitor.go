// Package monitor holds the pure domain logic for API health monitoring:
// state transitions and daily aggregate classification. It performs no I/O.
package monitor

import "time"

// Status is the health state of a monitored API.
type Status string

const (
	StatusPending  Status = "pending"
	StatusUp       Status = "up"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
)

// ProbeResult is the outcome of a single HTTP health probe.
type ProbeResult struct {
	Success    bool
	HTTPStatus int // 0 if no HTTP response was received
	LatencyMs  int
	ErrorKind  string // "" if Success; otherwise one of: timeout, dns, connection_refused, tls, http_status, blocked, other
	Commit     string // best-effort "commit" field from a JSON body; "" if absent or unparseable
}

// Next computes the new status and new consecutive-failure count given the
// current state and a probe result.
//
// Anti-false-positive policy: a single failure never changes the status by
// itself. It only increments the failure counter. The status only flips to
// down once the counter reaches threshold (a failure confirmed by the
// scheduler's retry-with-delay already happened before Next is called again
// with the retry's result).
func Next(current Status, failures int, result ProbeResult, degradedLatency time.Duration, threshold int) (newStatus Status, newFailures int) {
	if result.Success {
		if int64(result.LatencyMs) <= degradedLatency.Milliseconds() {
			return StatusUp, 0
		}
		return StatusDegraded, 0
	}

	newFailures = failures + 1
	if newFailures >= threshold {
		return StatusDown, newFailures
	}
	return current, newFailures
}

// DayState classifies a single day's aggregate health.
type DayState string

const (
	DayNone  DayState = "none"
	DayOp    DayState = "op"
	DayDeg   DayState = "deg"
	DayPart  DayState = "part"
	DayMajor DayState = "major"
)

// DayStatus classifies a day given its accumulated down/degraded seconds.
// checks is the number of recorded checks for that day; 0 means no data
// was recorded and the day is classified as "none" regardless of the
// (necessarily zero) seconds.
func DayStatus(downSeconds, degradedSeconds, checks int) DayState {
	if checks == 0 {
		return DayNone
	}
	if downSeconds >= 3600 {
		return DayMajor
	}
	if downSeconds > 0 {
		return DayPart
	}
	if degradedSeconds > 0 {
		return DayDeg
	}
	return DayOp
}

// Uptime computes the uptime percentage across daysWithData days, weighting
// down time fully and degraded time at 30%. Returns nil if there is no data
// (daysWithData == 0).
func Uptime(downSeconds, degradedSeconds, daysWithData int) *float64 {
	if daysWithData == 0 {
		return nil
	}

	downMinutes := float64(downSeconds) / 60
	degradedMinutes := float64(degradedSeconds) / 60
	pct := 100 - (downMinutes+degradedMinutes*0.3)/(float64(daysWithData)*1440)*100

	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return &pct
}
