import type { DayHistory, Monitor } from './api';

/** The availability windows the dashboard offers. The app always loads 90
 * days; a shorter period only slices that history at render time. */
export const PERIODS = [7, 30, 90] as const;

export type Period = (typeof PERIODS)[number];

export const DEFAULT_PERIOD: Period = 90;

export function isPeriod(value: unknown): value is Period {
	return typeof value === 'number' && (PERIODS as readonly number[]).includes(value);
}

/** Reads a stored period, falling back to the default for anything else. */
export function parsePeriod(raw: string | null): Period {
	const value = Number(raw);
	return isPeriod(value) ? value : DEFAULT_PERIOD;
}

/** A line-for-line port of monitor.Uptime in the Go API, so a sliced period
 * reports the same number the server would for that window: down time
 * counts fully, degraded time at 30 %, clamped to [0, 100], and null when
 * no day has data. */
export function uptime(downSeconds: number, degradedSeconds: number, daysWithData: number): number | null {
	if (daysWithData === 0) return null;

	const downMinutes = downSeconds / 60;
	const degradedMinutes = degradedSeconds / 60;
	const pct = 100 - ((downMinutes + degradedMinutes * 0.3) / (daysWithData * 1440)) * 100;

	return Math.min(100, Math.max(0, pct));
}

/** Uptime over the given history days, counted the way the server's
 * buildMonitorResponseFromStats does: a day has data when it has a stored
 * daily_stats row, which always carries at least one check, while a day
 * without a row comes back zero-valued (checks 0). */
export function historyUptime(history: DayHistory[]): number | null {
	let downSeconds = 0;
	let degradedSeconds = 0;
	let daysWithData = 0;
	for (const day of history) {
		if (day.checks === 0) continue;
		downSeconds += day.downMinutes * 60;
		degradedSeconds += day.degradedMinutes * 60;
		daysWithData++;
	}
	return uptime(downSeconds, degradedSeconds, daysWithData);
}

/** The monitors as seen through a period: the last `days` history entries
 * and the uptime recomputed over them. 90 is what the server already
 * computed, so it returns the very same objects. */
export function applyPeriod(monitors: Monitor[], days: Period): Monitor[] {
	if (days === DEFAULT_PERIOD) return monitors;
	return monitors.map((m) => {
		const history = m.history.slice(-days);
		return { ...m, history, uptimePercent: historyUptime(history) };
	});
}
