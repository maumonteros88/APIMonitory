import { describe, expect, it } from 'vitest';
import type { DayHistory, Monitor } from './api';
import { applyPeriod, historyUptime, isPeriod, parsePeriod, uptime } from './period';

function day(date: string, overrides: Partial<DayHistory> = {}): DayHistory {
	return { date, state: 'op', downMinutes: 0, degradedMinutes: 0, checks: 1440, fails: 0, ...overrides };
}

/** n consecutive days ending 2026-11-05, oldest first, like the API sends them. */
function days(n: number, make: (i: number, date: string) => DayHistory = (_, date) => day(date)): DayHistory[] {
	const out: DayHistory[] = [];
	for (let i = n - 1; i >= 0; i--) {
		const d = new Date(Date.UTC(2026, 10, 5 - i));
		out.push(make(n - 1 - i, d.toISOString().slice(0, 10)));
	}
	return out;
}

function makeMonitor(history: DayHistory[], uptimePercent: number | null = 99.5): Monitor {
	return {
		id: 'm',
		name: 'Pagos API',
		description: '',
		url: 'https://example.com/health',
		service: 'Pagos',
		company: null,
		kind: 'api',
		environment: 'prod',
		status: 'up',
		checking: false,
		lastCheck: null,
		commit: null,
		uptimePercent,
		history
	};
}

// The same cases as TestUptime in apps/api/internal/monitor/monitor_test.go.
describe('uptime (port of monitor.Uptime)', () => {
	it('returns null without data', () => {
		expect(uptime(0, 0, 0)).toBeNull();
	});

	it('is 100 with data and no incidents', () => {
		expect(uptime(0, 0, 30)).toBe(100);
	});

	it('counts a full day down out of 30 days', () => {
		expect(uptime(86400, 0, 30)).toBeCloseTo(100 - 100 / 30, 3);
	});

	it('weights degraded time at 30 percent', () => {
		expect(uptime(0, 3600, 1)).toBeCloseTo(100 - (18 / 1440) * 100, 3);
	});

	it('clamps to zero when fully down beyond the window', () => {
		expect(uptime(999999999, 0, 1)).toBe(0);
	});
});

describe('historyUptime', () => {
	it('counts only days with checks, like the server counts days with a stats row', () => {
		// 30 days with data, one of them fully down, plus 5 days without data
		// that must not dilute the result.
		const history = [
			...days(5, (_, date) => day(date, { state: 'none', checks: 0 })),
			...days(30, (i, date) => day(date, i === 10 ? { state: 'major', downMinutes: 1440 } : {}))
		];

		expect(historyUptime(history)).toBeCloseTo(100 - 100 / 30, 3);
	});

	it('weights degraded minutes at 30 percent', () => {
		expect(historyUptime([day('2026-11-05', { state: 'deg', degradedMinutes: 60 })])).toBeCloseTo(98.75, 3);
	});

	it('is null when no day has data', () => {
		expect(historyUptime(days(7, (_, date) => day(date, { state: 'none', checks: 0 })))).toBeNull();
	});
});

describe('applyPeriod', () => {
	const history = days(90, (i, date) => (i < 83 ? day(date, { state: 'major', downMinutes: 720 }) : day(date)));
	const monitor = makeMonitor(history, 50.5);

	it('keeps the server values at 90 days', () => {
		const [same] = applyPeriod([monitor], 90);
		expect(same).toBe(monitor);
	});

	it('slices the last days and recomputes the uptime over them', () => {
		const [week] = applyPeriod([monitor], 7);

		expect(week.history).toHaveLength(7);
		expect(week.history[0].date).toBe(history[83].date);
		expect(week.history[6].date).toBe('2026-11-05');
		expect(week.uptimePercent).toBe(100);
		// Never mutates the loaded monitor: SSE deltas keep merging into it.
		expect(monitor.history).toHaveLength(90);
		expect(monitor.uptimePercent).toBe(50.5);
	});

	it('reports no data for a period with no checks', () => {
		const fresh = makeMonitor(days(90, (_, date) => day(date, { state: 'none', checks: 0 })), null);
		expect(applyPeriod([fresh], 30)[0].uptimePercent).toBeNull();
	});
});

describe('isPeriod and parsePeriod', () => {
	it('accepts 7, 30 and 90 only', () => {
		for (const value of [7, 30, 90]) expect(isPeriod(value)).toBe(true);
		for (const value of [0, 14, '7', null]) expect(isPeriod(value)).toBe(false);
	});

	it('falls back to 90 for a missing or unknown stored value', () => {
		expect(parsePeriod('30')).toBe(30);
		expect(parsePeriod(null)).toBe(90);
		expect(parsePeriod('15')).toBe(90);
		expect(parsePeriod('abc')).toBe(90);
	});
});
