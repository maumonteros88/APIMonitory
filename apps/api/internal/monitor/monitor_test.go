package monitor

import (
	"testing"
	"time"
)

const threshold = 2
const degradedLatency = 2 * time.Second

func TestNext(t *testing.T) {
	tests := []struct {
		name         string
		current      Status
		failures     int
		result       ProbeResult
		wantStatus   Status
		wantFailures int
	}{
		{
			name:         "fast success from pending",
			current:      StatusPending,
			failures:     0,
			result:       ProbeResult{Success: true, LatencyMs: 100},
			wantStatus:   StatusUp,
			wantFailures: 0,
		},
		{
			name:         "fast success exactly at threshold latency is up",
			current:      StatusUp,
			failures:     0,
			result:       ProbeResult{Success: true, LatencyMs: 2000},
			wantStatus:   StatusUp,
			wantFailures: 0,
		},
		{
			name:         "slow success is degraded",
			current:      StatusUp,
			failures:     0,
			result:       ProbeResult{Success: true, LatencyMs: 2500},
			wantStatus:   StatusDegraded,
			wantFailures: 0,
		},
		{
			name:         "single failure from up does not change status",
			current:      StatusUp,
			failures:     0,
			result:       ProbeResult{Success: false, ErrorKind: "timeout"},
			wantStatus:   StatusUp,
			wantFailures: 1,
		},
		{
			name:         "single failure from degraded does not change status",
			current:      StatusDegraded,
			failures:     0,
			result:       ProbeResult{Success: false, ErrorKind: "timeout"},
			wantStatus:   StatusDegraded,
			wantFailures: 1,
		},
		{
			name:         "second consecutive failure trips to down",
			current:      StatusUp,
			failures:     1,
			result:       ProbeResult{Success: false, ErrorKind: "timeout"},
			wantStatus:   StatusDown,
			wantFailures: 2,
		},
		{
			name:         "recovery from down with one success",
			current:      StatusDown,
			failures:     2,
			result:       ProbeResult{Success: true, LatencyMs: 50},
			wantStatus:   StatusUp,
			wantFailures: 0,
		},
		{
			name:         "initial pending state, first failure",
			current:      StatusPending,
			failures:     0,
			result:       ProbeResult{Success: false, ErrorKind: "dns"},
			wantStatus:   StatusPending,
			wantFailures: 1,
		},
		{
			name:         "already down, another failure stays down and keeps counting",
			current:      StatusDown,
			failures:     2,
			result:       ProbeResult{Success: false, ErrorKind: "timeout"},
			wantStatus:   StatusDown,
			wantFailures: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, gotFailures := Next(tt.current, tt.failures, tt.result, degradedLatency, threshold)
			if gotStatus != tt.wantStatus {
				t.Errorf("status = %q, want %q", gotStatus, tt.wantStatus)
			}
			if gotFailures != tt.wantFailures {
				t.Errorf("failures = %d, want %d", gotFailures, tt.wantFailures)
			}
		})
	}
}

func TestDayStatus(t *testing.T) {
	tests := []struct {
		name            string
		downSeconds     int
		degradedSeconds int
		checks          int
		want            DayState
	}{
		{"no checks means no data", 0, 0, 0, DayNone},
		{"no data even with stray seconds recorded", 100, 0, 0, DayNone},
		{"fully operational", 0, 0, 1440, DayOp},
		{"degraded only", 0, 300, 1440, DayDeg},
		{"partial outage below major threshold", 3599, 0, 1440, DayPart},
		{"exactly at major threshold", 3600, 0, 1440, DayMajor},
		{"above major threshold", 7200, 0, 1440, DayMajor},
		{"down takes priority over degraded", 10, 500, 1440, DayPart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DayStatus(tt.downSeconds, tt.degradedSeconds, tt.checks)
			if got != tt.want {
				t.Errorf("DayStatus(%d, %d, %d) = %q, want %q", tt.downSeconds, tt.degradedSeconds, tt.checks, got, tt.want)
			}
		})
	}
}

func TestUptime(t *testing.T) {
	tests := []struct {
		name            string
		downSeconds     int
		degradedSeconds int
		daysWithData    int
		want            *float64
	}{
		{"no data returns nil", 0, 0, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uptime(tt.downSeconds, tt.degradedSeconds, tt.daysWithData)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("Uptime() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("perfect uptime with data", func(t *testing.T) {
		got := Uptime(0, 0, 30)
		if got == nil || *got != 100 {
			t.Fatalf("Uptime(0, 0, 30) = %v, want 100", got)
		}
	})

	t.Run("full day down out of 30 days", func(t *testing.T) {
		got := Uptime(86400, 0, 30)
		if got == nil {
			t.Fatalf("Uptime() = nil, want a value")
		}
		// 1440 minutes down out of 30*1440 minutes = 1/30 = 3.333...%
		want := 100 - (100.0 / 30.0)
		if diff := *got - want; diff > 0.001 || diff < -0.001 {
			t.Fatalf("Uptime(86400, 0, 30) = %v, want %v", *got, want)
		}
	})

	t.Run("degraded weighted at 30 percent", func(t *testing.T) {
		got := Uptime(0, 3600, 1)
		if got == nil {
			t.Fatalf("Uptime() = nil, want a value")
		}
		// 60 minutes degraded * 0.3 = 18 minutes equivalent, out of 1440
		want := 100 - (18.0/1440.0)*100
		if diff := *got - want; diff > 0.001 || diff < -0.001 {
			t.Fatalf("Uptime(0, 3600, 1) = %v, want %v", *got, want)
		}
	})

	t.Run("clamped to zero when fully down beyond window", func(t *testing.T) {
		got := Uptime(999999999, 0, 1)
		if got == nil || *got != 0 {
			t.Fatalf("Uptime() = %v, want 0", got)
		}
	})
}
