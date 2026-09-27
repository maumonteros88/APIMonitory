import { describe, expect, it } from 'vitest';
import { mergeMonitors, upsertMonitor, type Monitor } from './api';

function makeMonitor(id: string, at: string | null): Monitor {
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
		history: []
	};
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
