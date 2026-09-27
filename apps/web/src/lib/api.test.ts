import { describe, expect, it } from 'vitest';
import {
	applyMonitorDelta,
	mergeMonitors,
	mergeRecentMonitors,
	upsertMonitor,
	type DayHistory,
	type Monitor
} from './api';

function makeMonitor(id: string, at: string | null, history: DayHistory[] = []): Monitor {
	return {
		id,
		name: id,
		description: '',
		url: 'https://example.com',
		status: 'up',
		checking: false,
		lastCheck: at ? { at, httpStatus: 200, latencyMs: 10, errorKind: '' } : null,
		commit: null,
		uptimePercent: 100,
		history
	};
}

function makeDay(date: string, overrides: Partial<DayHistory> = {}): DayHistory {
	return { date, state: 'op', downMinutes: 0, degradedMinutes: 0, checks: 1, fails: 0, ...overrides };
}

/** Asserts history is strictly ascending by date, has no duplicate dates,
 * and is contiguous day-to-day -- the invariant every merge path (delta or
 * full) must preserve. expectedLength additionally checks the window's
 * length didn't change, which every delta-path merge must also preserve. */
function assertWindowInvariant(history: DayHistory[], expectedLength?: number) {
	if (expectedLength !== undefined) {
		expect(history.length).toBe(expectedLength);
	}
	const dates = history.map((d) => d.date);
	expect(new Set(dates).size).toBe(dates.length);
	for (let i = 1; i < dates.length; i++) {
		expect(dates[i] > dates[i - 1]).toBe(true);
		const prev = new Date(`${dates[i - 1]}T00:00:00Z`);
		prev.setUTCDate(prev.getUTCDate() + 1);
		expect(dates[i]).toBe(prev.toISOString().slice(0, 10));
	}
}

describe('upsertMonitor', () => {
	it('appends a monitor not already in the list', () => {
		const list = [makeMonitor('a', '2026-01-01T00:00:00Z')];
		const b = makeMonitor('b', '2026-01-01T00:00:00Z');

		expect(upsertMonitor(list, b)).toEqual([list[0], b]);
	});

	it('replaces an existing monitor with a newer lastCheck', () => {
		const list = [makeMonitor('a', '2026-01-01T00:00:00Z')];
		const updated = makeMonitor('a', '2026-01-01T00:01:00Z');

		expect(upsertMonitor(list, updated)).toEqual([updated]);
	});

	it('keeps the existing monitor if the incoming one is older (poll/event race)', () => {
		const current = makeMonitor('a', '2026-01-01T00:05:00Z');
		const stale = makeMonitor('a', '2026-01-01T00:01:00Z');

		expect(upsertMonitor([current], stale)).toEqual([current]);
	});

	it('replaces a pending monitor (no lastCheck yet) unconditionally', () => {
		const pending = makeMonitor('a', null);
		const checked = makeMonitor('a', '2026-01-01T00:00:00Z');

		expect(upsertMonitor([pending], checked)).toEqual([checked]);
	});
});

describe('mergeMonitors', () => {
	it('adopts a newly created monitor not yet in the current list', () => {
		const fresh = makeMonitor('b', '2026-01-01T00:00:00Z');

		expect(mergeMonitors([], [fresh])).toEqual([fresh]);
	});

	it('drops a monitor missing from the fetched list (deleted server-side)', () => {
		const current = [makeMonitor('a', '2026-01-01T00:00:00Z')];

		expect(mergeMonitors(current, [])).toEqual([]);
	});

	it('keeps the current entry when the fetched one is older than an SSE update already applied', () => {
		const current = [makeMonitor('a', '2026-01-01T00:05:00Z')];
		const fetched = [makeMonitor('a', '2026-01-01T00:01:00Z')];

		expect(mergeMonitors(current, fetched)).toEqual(current);
	});

	it('adopts the fetched entry when it is newer than the current one', () => {
		const current = [makeMonitor('a', '2026-01-01T00:00:00Z')];
		const fetched = [makeMonitor('a', '2026-01-01T00:05:00Z')];

		expect(mergeMonitors(current, fetched)).toEqual(fetched);
	});
});

describe('applyMonitorDelta', () => {
	it('applies a 2-day delta crossing midnight: replaces yesterday in place, appends today, drops the oldest', () => {
		const existingHistory = [
			makeDay('2026-01-01'),
			makeDay('2026-01-02'),
			makeDay('2026-01-03'),
			makeDay('2026-01-04'),
			makeDay('2026-01-05', { checks: 1 })
		];
		const current = [makeMonitor('a', '2026-01-05T23:59:00Z', existingHistory)];
		const update = makeMonitor('a', '2026-01-06T00:05:00Z', [
			makeDay('2026-01-05', { checks: 2 }),
			makeDay('2026-01-06', { checks: 1 })
		]);

		const result = applyMonitorDelta(current, update);

		expect(result.needsResync).toBe(false);
		const history = result.monitors[0].history;
		assertWindowInvariant(history, 5);
		expect(history.map((d) => d.date)).toEqual([
			'2026-01-02',
			'2026-01-03',
			'2026-01-04',
			'2026-01-05',
			'2026-01-06'
		]);
		expect(history[3].checks).toBe(2); // yesterday, replaced in place
		expect(history[4].checks).toBe(1); // today, appended
	});

	it('applies a same-day delta as an in-place replace, with no reordering', () => {
		const existingHistory = [makeDay('2026-01-04'), makeDay('2026-01-05', { checks: 1 })];
		const current = [makeMonitor('a', '2026-01-05T10:00:00Z', existingHistory)];
		const update = makeMonitor('a', '2026-01-05T12:00:00Z', [
			makeDay('2026-01-04'),
			makeDay('2026-01-05', { checks: 3 })
		]);

		const result = applyMonitorDelta(current, update);

		expect(result.needsResync).toBe(false);
		const history = result.monitors[0].history;
		assertWindowInvariant(history, 2);
		expect(history.map((d) => d.date)).toEqual(['2026-01-04', '2026-01-05']);
		expect(history[1].checks).toBe(3);
	});

	it('flags needsResync when the delta has a gap past the local window', () => {
		const current = [
			makeMonitor('a', '2026-01-05T00:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')])
		];
		const update = makeMonitor('a', '2026-01-08T00:00:00Z', [makeDay('2026-01-08'), makeDay('2026-01-09')]);

		const result = applyMonitorDelta(current, update);

		expect(result).toEqual({ monitors: current, needsResync: true });
	});

	it("flags needsResync when the delta's first date is not locally present, even if it is exactly the next day", () => {
		const current = [
			makeMonitor('a', '2026-01-05T00:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')])
		];
		const update = makeMonitor('a', '2026-01-06T00:00:00Z', [makeDay('2026-01-06'), makeDay('2026-01-07')]);

		const result = applyMonitorDelta(current, update);

		expect(result).toEqual({ monitors: current, needsResync: true });
	});

	it('applies a repeated crossing-midnight delta as an in-place replace once today is already present, without reordering', () => {
		const existingHistory = [
			makeDay('2026-01-02'),
			makeDay('2026-01-03'),
			makeDay('2026-01-04'),
			makeDay('2026-01-05'),
			makeDay('2026-01-06')
		];
		const current = [makeMonitor('a', '2026-01-06T00:10:00Z', existingHistory)];
		// A late/duplicate delivery of the same crossing delta -- both of its
		// dates are already present, so this must not append or reorder.
		const update = makeMonitor('a', '2026-01-06T00:15:00Z', [
			makeDay('2026-01-05', { checks: 9 }),
			makeDay('2026-01-06', { checks: 9 })
		]);

		const result = applyMonitorDelta(current, update);

		expect(result.needsResync).toBe(false);
		const history = result.monitors[0].history;
		assertWindowInvariant(history, 5);
		expect(history.map((d) => d.date)).toEqual([
			'2026-01-02',
			'2026-01-03',
			'2026-01-04',
			'2026-01-05',
			'2026-01-06'
		]);
		expect(history[3].checks).toBe(9);
		expect(history[4].checks).toBe(9);
	});

	it('flags needsResync for an unknown monitor id', () => {
		const current = [
			makeMonitor('a', '2026-01-05T00:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')])
		];
		const update = makeMonitor('b', '2026-01-06T00:00:00Z', [makeDay('2026-01-05'), makeDay('2026-01-06')]);

		const result = applyMonitorDelta(current, update);

		expect(result).toEqual({ monitors: current, needsResync: true });
	});

	it('leaves the list unchanged (not a resync) when the delta lastCheck is older', () => {
		const current = [
			makeMonitor('a', '2026-01-06T00:10:00Z', [makeDay('2026-01-05'), makeDay('2026-01-06')])
		];
		const update = makeMonitor('a', '2026-01-05T23:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')]);

		const result = applyMonitorDelta(current, update);

		expect(result).toEqual({ monitors: current, needsResync: false });
	});

	it('applies the delta when the update has no lastCheck yet (pending)', () => {
		const current = [makeMonitor('a', null, [makeDay('2026-01-04'), makeDay('2026-01-05')])];
		const update = makeMonitor('a', null, [makeDay('2026-01-05'), makeDay('2026-01-06')]);

		const result = applyMonitorDelta(current, update);

		expect(result.needsResync).toBe(false);
		expect(result.monitors[0].history.map((d) => d.date)).toEqual(['2026-01-05', '2026-01-06']);
	});

	it('applies the delta when only the existing lastCheck is null', () => {
		const current = [makeMonitor('a', null, [makeDay('2026-01-04'), makeDay('2026-01-05')])];
		const update = makeMonitor('a', '2026-01-06T00:00:00Z', [makeDay('2026-01-05'), makeDay('2026-01-06')]);

		const result = applyMonitorDelta(current, update);

		expect(result.needsResync).toBe(false);
	});
});

describe('mergeRecentMonitors', () => {
	it('drops a monitor missing from fetched (deleted server-side)', () => {
		const a = makeMonitor('a', '2026-01-05T00:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')]);
		const b = makeMonitor('b', '2026-01-05T00:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')]);
		const fetchedB = makeMonitor('b', '2026-01-06T00:00:00Z', [makeDay('2026-01-05'), makeDay('2026-01-06')]);

		const result = mergeRecentMonitors([a, b], [fetchedB]);

		expect(result.needsResync).toBe(false);
		expect(result.monitors.map((m) => m.id)).toEqual(['b']);
		assertWindowInvariant(result.monitors[0].history, 2);
	});

	it('propagates needsResync by returning current unchanged when any delta fails', () => {
		const current = [
			makeMonitor('a', '2026-01-05T00:00:00Z', [makeDay('2026-01-04'), makeDay('2026-01-05')])
		];
		// A gap: fetched jumps past the local window.
		const fetchedA = makeMonitor('a', '2026-01-08T00:00:00Z', [makeDay('2026-01-08'), makeDay('2026-01-09')]);

		const result = mergeRecentMonitors(current, [fetchedA]);

		expect(result).toEqual({ monitors: current, needsResync: true });
	});

	it('flags needsResync for a fetched monitor unknown locally (created between polls)', () => {
		const current: Monitor[] = [];
		const fetchedNew = makeMonitor('new', '2026-01-05T00:00:00Z', [
			makeDay('2026-01-04'),
			makeDay('2026-01-05')
		]);

		const result = mergeRecentMonitors(current, [fetchedNew]);

		expect(result).toEqual({ monitors: current, needsResync: true });
	});
});
