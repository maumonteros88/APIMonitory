import { describe, expect, it } from 'vitest';
import type { Environment, LatencyPoint, Monitor, MonitorKind } from './api';
import {
	bucketDate,
	buildChartGroups,
	buildSeries,
	cardState,
	cardSummary,
	chartAriaLabel,
	downBucketsLabel,
	formatAge,
	formatAxisMs,
	formatBucketTime,
	formatMs,
	groupMeta,
	lastValue,
	lineRuns,
	niceMax,
	parseLatencyRange,
	peakOf,
	pointLabel,
	polylinePoints,
	REFRESH_MS,
	spreadLabels,
	staleAfterMs,
	summarize,
	toLatencyData,
	xOf,
	yOf
} from './chart';

function makeMonitor(
	id: string,
	service: string | null,
	kind: MonitorKind,
	environment: Environment | null,
	status: Monitor['status'] = 'up',
	name = id
): Monitor {
	return {
		id,
		name,
		description: '',
		url: 'https://example.com',
		service,
		kind,
		environment,
		status,
		checking: false,
		lastCheck: null,
		commit: null,
		uptimePercent: 100,
		history: []
	};
}

function point(avgMs: number | null, overrides: Partial<LatencyPoint> = {}): LatencyPoint {
	return {
		at: '2026-11-05T00:00:00Z',
		avgMs,
		maxMs: avgMs,
		checks: avgMs === null ? 0 : 15,
		status: avgMs === null ? 'none' : 'up',
		...overrides
	};
}

describe('parseLatencyRange', () => {
	it('keeps a known range and falls back to 24h for anything else', () => {
		expect(parseLatencyRange('7d')).toBe('7d');
		expect(parseLatencyRange('30d')).toBe('30d');
		expect(parseLatencyRange(null)).toBe('24h');
		expect(parseLatencyRange('90d')).toBe('24h');
	});
});

describe('buildChartGroups', () => {
	const monitors = [
		makeMonitor('pagos-api-dev', 'Pagos', 'api', 'dev'),
		makeMonitor('pagos-api-prod', 'Pagos', 'api', 'prod', 'down'),
		makeMonitor('pagos-front-prod', 'Pagos', 'front', 'prod'),
		makeMonitor('pagos-api-qa', 'Pagos', 'api', 'qa'),
		makeMonitor('pagos-api-none', 'Pagos', 'api', null),
		makeMonitor('bo-front-prod', 'Backoffice', 'front', 'prod', 'degraded'),
		makeMonitor('loose-api', null, 'api', null)
	];

	it('groups by service alphabetically with "Sin servicio" last', () => {
		expect(buildChartGroups(monitors).map((g) => g.service)).toEqual(['Backoffice', 'Pagos', null]);
	});

	it('orders a service cards prod, qa, dev, then no environment', () => {
		const pagos = buildChartGroups(monitors).find((g) => g.service === 'Pagos')!;
		expect(pagos.cards.map((c) => c.environment)).toEqual(['prod', 'qa', 'dev', null]);
		expect(pagos.cards.map((c) => c.key)).toEqual(['Pagos|prod', 'Pagos|qa', 'Pagos|dev', 'Pagos|']);
	});

	it('puts the front before the API inside a card', () => {
		const pagos = buildChartGroups(monitors).find((g) => g.service === 'Pagos')!;
		expect(pagos.cards[0].monitors.map((m) => m.id)).toEqual(['pagos-front-prod', 'pagos-api-prod']);
	});

	it('carries the worst status and the heading text', () => {
		const groups = buildChartGroups(monitors);
		expect(groups.map((g) => g.worst)).toEqual(['degraded', 'down', 'up']);
		expect(groups.map((g) => g.meta.text)).toEqual(['1 degradado', '1 caído', 'Operativo']);
	});
});

describe('groupMeta', () => {
	it('counts down before degraded, with plurals', () => {
		expect(groupMeta([makeMonitor('a', 'S', 'api', 'prod', 'down'), makeMonitor('b', 'S', 'api', 'qa', 'down')])).toEqual({
			text: '2 caídos',
			tone: 'bad'
		});
		expect(
			groupMeta([makeMonitor('a', 'S', 'api', 'prod', 'degraded'), makeMonitor('b', 'S', 'api', 'qa', 'degraded')])
		).toEqual({ text: '2 degradados', tone: 'warn' });
	});

	it('does not call a group of pending monitors operational', () => {
		expect(groupMeta([makeMonitor('a', 'S', 'api', 'prod', 'pending')])).toEqual({ text: 'Sin verificar', tone: 'plain' });
	});
});

describe('buildSeries', () => {
	it('labels each kind once and draws fronts first', () => {
		const series = buildSeries([makeMonitor('api', 'S', 'api', 'prod'), makeMonitor('front', 'S', 'front', 'prod')]);
		expect(series).toEqual([
			{ id: 'front', kind: 'front', label: 'Front', dash: null },
			{ id: 'api', kind: 'api', label: 'API', dash: null }
		]);
	});

	it('names and dashes the second monitor of the same kind', () => {
		const series = buildSeries([
			makeMonitor('front', 'S', 'front', 'prod', 'up', 'Pagos Web'),
			makeMonitor('api-1', 'S', 'api', 'prod', 'up', 'Pagos API'),
			makeMonitor('api-2', 'S', 'api', 'prod', 'up', 'Pagos API respaldo')
		]);
		expect(series.map((s) => s.label)).toEqual(['Front', 'Pagos API', 'Pagos API respaldo']);
		expect(series.map((s) => s.dash)).toEqual([null, null, '6 6']);
	});
});

describe('toLatencyData', () => {
	it('indexes the monitors by id, whatever order they come in', () => {
		const data = toLatencyData({
			range: '7d',
			bucketMinutes: 120,
			from: '2026-10-29T12:00:00Z',
			to: '2026-11-05T12:00:00Z',
			monitors: [
				{ id: 'b', points: [point(20)] },
				{ id: 'a', points: [point(10)] }
			]
		});
		expect(data.range).toBe('7d');
		expect(data.byId.get('a')?.[0].avgMs).toBe(10);
		expect(data.byId.get('b')?.[0].avgMs).toBe(20);
	});
});

describe('cardState', () => {
	const data = toLatencyData({
		range: '24h',
		bucketMinutes: 15,
		from: '2026-11-04T00:00:00Z',
		to: '2026-11-05T00:00:00Z',
		monitors: [{ id: 'a', points: [point(10)] }]
	});

	it('shows a card whose monitors are all loaded, even while a new request runs', () => {
		expect(cardState(['a'], data, true, false)).toBe('ready');
	});

	it('waits for a monitor missing from the data while a request runs', () => {
		expect(cardState(['a', 'b'], data, true, false)).toBe('loading');
		expect(cardState(['a'], null, false, false)).toBe('loading');
	});

	it('shows the error only for cards that have nothing to draw', () => {
		expect(cardState(['b'], null, false, true)).toBe('error');
		expect(cardState(['a'], data, false, true)).toBe('ready');
	});

	it('draws a monitor the server does not know once nothing is pending', () => {
		expect(cardState(['a', 'gone'], data, false, false)).toBe('ready');
	});
});

describe('niceMax', () => {
	it('picks the first step at or above the peak plus 10 %', () => {
		expect(niceMax(0)).toBe(50);
		expect(niceMax(45)).toBe(50);
		expect(niceMax(46)).toBe(100);
		expect(niceMax(90)).toBe(100);
		expect(niceMax(91)).toBe(250);
		expect(niceMax(220)).toBe(250);
		expect(niceMax(400)).toBe(500);
		expect(niceMax(900)).toBe(1000);
		expect(niceMax(2000)).toBe(2500);
		expect(niceMax(4500)).toBe(5000);
		expect(niceMax(4600)).toBe(10000);
		expect(niceMax(9090)).toBe(10000);
	});

	it('keeps going past 10 s instead of clipping the line', () => {
		expect(niceMax(12000)).toBe(25000);
	});

	it('never loops on a non-finite peak', () => {
		expect(niceMax(Number.POSITIVE_INFINITY)).toBe(50);
	});
});

describe('geometry', () => {
	it('puts the first bucket on the left edge and the last on the right', () => {
		expect(xOf(0, 96)).toBe(0);
		expect(xOf(95, 96)).toBe(100);
		expect(yOf(0, 250)).toBe(100);
		expect(yOf(250, 250)).toBe(0);
	});

	it('breaks the line at a null instead of drawing it to zero', () => {
		const runs = lineRuns([100, 100, null, 50, 50], 100);
		expect(runs).toEqual([
			[
				{ x: 0, y: 0 },
				{ x: 25, y: 0 }
			],
			[
				{ x: 75, y: 50 },
				{ x: 100, y: 50 }
			]
		]);
	});

	it('keeps an isolated value as a run of one point', () => {
		expect(lineRuns([null, 100, null], 100)).toEqual([[{ x: 50, y: 0 }]]);
		expect(lineRuns([null, null], 100)).toEqual([]);
	});

	it('formats polyline points', () => {
		expect(polylinePoints([{ x: 0, y: 12.344 }, { x: 50, y: 0 }])).toBe('0.00,12.34 50.00,0.00');
	});

	it('finds the peak and the last value', () => {
		expect(peakOf([[10, null, 30], [null, 25]])).toBe(30);
		expect(peakOf([[null]])).toBe(0);
		expect(lastValue([10, 20, null, null])).toBe(20);
		expect(lastValue([null, null])).toBeNull();
	});
});

describe('spreadLabels', () => {
	it('leaves labels that are far apart alone', () => {
		expect(spreadLabels([20, 70], 14, 4, 96)).toEqual([20, 70]);
	});

	it('pushes two close labels apart around their middle', () => {
		expect(spreadLabels([52, 48], 14, 4, 96)).toEqual([57, 43]);
	});

	it('spreads three colliding labels (a front and two APIs)', () => {
		const out = spreadLabels([50, 52, 48], 14, 4, 96);
		const sorted = out.slice().sort((a, b) => a - b);
		expect(sorted[1] - sorted[0]).toBeCloseTo(14);
		expect(sorted[2] - sorted[1]).toBeCloseTo(14);
		// Input order is kept: the lowest value stays the topmost label.
		expect(out[2]).toBe(sorted[0]);
		expect(out[1]).toBe(sorted[2]);
	});

	it('keeps clusters inside the bounds without overlapping', () => {
		expect(spreadLabels([0, 1], 14, 4, 96)).toEqual([4, 18]);
		expect(spreadLabels([99, 100], 14, 4, 96)).toEqual([82, 96]);
	});

	function expectSpread(out: number[], lo: number, hi: number) {
		const sorted = out.slice().sort((a, b) => a - b);
		expect(sorted[0]).toBeGreaterThanOrEqual(lo - 1e-9);
		expect(sorted[sorted.length - 1]).toBeLessThanOrEqual(hi + 1e-9);
		for (let k = 1; k < sorted.length; k++) expect(sorted[k] - sorted[k - 1]).toBeGreaterThanOrEqual(10);
	}

	it('compresses the gap to fit 8 labels at the same value', () => {
		const out = spreadLabels(Array(8).fill(50), 14, 4, 96);
		expectSpread(out, 4, 96);
		// 7 gaps over 92: evenly spread from one end of the plot to the other.
		const sorted = out.slice().sort((a, b) => a - b);
		expect(sorted[0]).toBeCloseTo(4);
		expect(sorted[7]).toBeCloseTo(96);
		expect(sorted[1] - sorted[0]).toBeCloseTo(92 / 7);
	});

	it('keeps 8 labels inside the bounds near the top and bottom edges', () => {
		expectSpread(spreadLabels(Array(8).fill(0), 14, 4, 96), 4, 96);
		expectSpread(spreadLabels(Array(8).fill(100), 14, 4, 96), 4, 96);
		expectSpread(spreadLabels([0, 1, 2, 3, 97, 98, 99, 100], 14, 4, 96), 4, 96);
	});

	it('keeps 5 labels pushed against the bottom edge inside hi', () => {
		const out = spreadLabels([100, 100, 100, 100, 100], 14, 4, 96);
		expectSpread(out, 4, 96);
		expect(Math.max(...out)).toBeCloseTo(96);
	});

	it('handles no labels and a single label', () => {
		expect(spreadLabels([], 14, 4, 96)).toEqual([]);
		expect(spreadLabels([100], 14, 4, 96)).toEqual([96]);
	});
});

describe('refresh cadence', () => {
	it('refreshes every 60 s, 5 min and 15 min for 24 h, 7 d and 30 d', () => {
		expect(REFRESH_MS['24h']).toBe(60_000);
		expect(REFRESH_MS['7d']).toBe(300_000);
		expect(REFRESH_MS['30d']).toBe(900_000);
	});

	it('marks data stale after two refresh intervals', () => {
		expect(staleAfterMs('24h')).toBe(120_000);
		expect(staleAfterMs('7d')).toBe(600_000);
		expect(staleAfterMs('30d')).toBe(1_800_000);
	});
});

describe('formatAge', () => {
	it('reads minutes, hours and days', () => {
		expect(formatAge(20_000)).toBe('menos de 1 min');
		expect(formatAge(-5)).toBe('menos de 1 min');
		expect(formatAge(5 * 60_000 + 10_000)).toBe('5 min');
		expect(formatAge(59 * 60_000)).toBe('59 min');
		expect(formatAge(3 * 3_600_000 + 60_000)).toBe('3 h');
		expect(formatAge(24 * 3_600_000)).toBe('1 día');
		expect(formatAge(50 * 3_600_000)).toBe('2 días');
	});
});

describe('formatMs', () => {
	it('shows milliseconds below one second and seconds from there on', () => {
		expect(formatMs(120)).toBe('120 ms');
		expect(formatMs(120.4)).toBe('120 ms');
		expect(formatMs(999.6)).toBe('1 s');
		expect(formatMs(1200)).toBe('1,2 s');
		expect(formatMs(2000)).toBe('2 s');
		expect(formatMs(12340)).toBe('12,3 s');
	});

	it('keeps two decimals on the axis', () => {
		expect(formatAxisMs(125)).toBe('125 ms');
		expect(formatAxisMs(1250)).toBe('1,25 s');
		expect(formatAxisMs(2500)).toBe('2,5 s');
		expect(formatAxisMs(10000)).toBe('10 s');
	});
});

describe('formatBucketTime', () => {
	// The local constructor keeps these independent of the machine's zone.
	const date = new Date(2026, 10, 5, 14, 15);

	it('formats per range in Spanish', () => {
		expect(formatBucketTime(date, '24h')).toBe('14:15');
		expect(formatBucketTime(date, '7d')).toBe('jue 14:15');
		expect(formatBucketTime(date, '30d')).toBe('5 nov 14:15');
		expect(formatBucketTime(new Date(2026, 0, 4, 9, 0), '7d')).toBe('dom 09:00');
	});

	it('derives a bucket start from the window', () => {
		expect(bucketDate('2026-11-05T00:00:00Z', 15, 4).toISOString()).toBe('2026-11-05T01:00:00.000Z');
	});
});

describe('pointLabel', () => {
	it('shows the value, marking degraded buckets', () => {
		expect(pointLabel(point(120))).toBe('120 ms');
		expect(pointLabel(point(1200, { status: 'degraded' }))).toBe('1,2 s · degradada');
	});

	it('tells failed checks apart from no checks', () => {
		expect(pointLabel(point(null, { checks: 4, status: 'down' }))).toBe('Sin respuesta');
		expect(pointLabel(point(null))).toBe('Sin datos');
		expect(pointLabel(undefined)).toBe('Sin datos');
	});
});

describe('summarize', () => {
	const points = [
		point(100, { maxMs: 300 }),
		point(200, { maxMs: 250, status: 'degraded' }),
		point(null, { checks: 3, status: 'down' }),
		point(null)
	];

	it('averages the non-null buckets and takes the max of maxMs', () => {
		expect(summarize(points)).toEqual({ avgMs: 150, maxMs: 300, downBuckets: 1, checkedBuckets: 3 });
	});

	it('counts down buckets over the buckets with checks', () => {
		expect(downBucketsLabel(summarize(points))).toBe('1 de 3');
		expect(downBucketsLabel(summarize([point(null), point(null)]))).toBe('Sin datos');
	});

	it('summarizes a card in one line', () => {
		const series = buildSeries([makeMonitor('f', 'S', 'front', 'prod'), makeMonitor('a', 'S', 'api', 'prod')]);
		expect(cardSummary(series, [summarize(points), summarize([point(null)])])).toBe(
			'Promedio: Front 150 ms · API sin datos'
		);
	});
});

describe('chartAriaLabel', () => {
	it('names what the chart plots', () => {
		const series = buildSeries([makeMonitor('f', 'Pagos', 'front', 'prod'), makeMonitor('a', 'Pagos', 'api', 'prod')]);
		expect(chartAriaLabel('Pagos', 'prod', series, '24h')).toBe(
			'Latencia de Front y API de Pagos en Prod, promedio cada 15 minutos, últimas 24 horas'
		);
		expect(chartAriaLabel(null, null, series.slice(1), '7d')).toBe(
			'Latencia de API, sin servicio, sin ambiente, promedio cada 2 horas, últimos 7 días'
		);
	});
});
