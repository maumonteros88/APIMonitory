import type { Environment, LatencyPoint, LatencyRange, LatencyResponse, Monitor, MonitorKind } from './api';
import { worstStatus, type WorstStatus } from './filters';
import { MONTHS_ES } from './format';
import { ENV_LABELS, KIND_LABELS, groupByService } from './grouping';

export const LATENCY_RANGES: LatencyRange[] = ['24h', '7d', '30d'];

export const DEFAULT_LATENCY_RANGE: LatencyRange = '24h';

export interface RangeInfo {
	/** The range control's option. */
	label: string;
	/** How many buckets the server returns for the range. */
	buckets: number;
	caption: string;
	/** The x axis labels at the left edge and the middle; the right edge is "Ahora". */
	xStart: string;
	xMid: string;
}

export const RANGE_INFO: Record<LatencyRange, RangeInfo> = {
	'24h': {
		label: '24 h',
		buckets: 96,
		caption: 'Promedio cada 15 minutos, últimas 24 horas',
		xStart: 'Hace 24 h',
		xMid: 'Hace 12 h'
	},
	'7d': {
		label: '7 días',
		buckets: 84,
		caption: 'Promedio cada 2 horas, últimos 7 días',
		xStart: 'Hace 7 días',
		xMid: 'Hace 3,5 días'
	},
	'30d': {
		label: '30 días',
		buckets: 60,
		caption: 'Promedio cada 12 horas, últimos 30 días',
		xStart: 'Hace 30 días',
		xMid: 'Hace 15 días'
	}
};

/** How often the open chart view refreshes in the background: no faster than
 * the range's buckets can change (15 min, 2 h and 12 h wide), and slower for
 * the long ranges, whose responses aggregate up to 30 days of checks. */
export const REFRESH_MS: Record<LatencyRange, number> = {
	'24h': 60_000,
	'7d': 5 * 60_000,
	'30d': 15 * 60_000
};

/** Data older than this (two refresh intervals) is shown as stale. */
export function staleAfterMs(range: LatencyRange): number {
	return REFRESH_MS[range] * 2;
}

/** "menos de 1 min", "5 min", "3 h", "2 días": how old the data on screen is. */
export function formatAge(ms: number): string {
	const minutes = Math.floor(Math.max(0, ms) / 60_000);
	if (minutes < 1) return 'menos de 1 min';
	if (minutes < 60) return `${minutes} min`;
	const hours = Math.floor(minutes / 60);
	if (hours < 24) return `${hours} h`;
	const days = Math.floor(hours / 24);
	return days === 1 ? '1 día' : `${days} días`;
}

export function isLatencyRange(value: unknown): value is LatencyRange {
	return typeof value === 'string' && (LATENCY_RANGES as string[]).includes(value);
}

/** Reads a stored range, falling back to the default for anything else. */
export function parseLatencyRange(raw: string | null): LatencyRange {
	return isLatencyRange(raw) ? raw : DEFAULT_LATENCY_RANGE;
}

// ---------- Data ----------

export interface LatencyData {
	range: LatencyRange;
	from: string;
	bucketMinutes: number;
	byId: Map<string, LatencyPoint[]>;
}

/** Indexes a response by monitor id: the server answers in creation order
 * and silently leaves out ids it does not know. */
export function toLatencyData(res: LatencyResponse): LatencyData {
	return {
		range: res.range,
		from: res.from,
		bucketMinutes: res.bucketMinutes,
		byId: new Map(res.monitors.map((m) => [m.id, m.points]))
	};
}

export type CardState = 'ready' | 'loading' | 'error';

/** What a card shows. data is the last response for the current range (null
 * when there is none yet); loading and failed describe the latest request. A
 * card whose monitors are all in data keeps showing it while a request for a
 * new id set is in flight; a monitor the server left out is drawn without
 * data once nothing is pending. */
export function cardState(ids: string[], data: LatencyData | null, loading: boolean, failed: boolean): CardState {
	if (data && ids.every((id) => data.byId.has(id))) return 'ready';
	if (loading) return 'loading';
	if (failed) return 'error';
	return data ? 'ready' : 'loading';
}

// ---------- Cards ----------

export interface GroupMeta {
	text: string;
	tone: 'bad' | 'warn' | 'plain';
}

/** The service heading's status text, from the monitors' current status
 * (the same one the banners use), not from the chart's buckets. */
export function groupMeta(monitors: Monitor[]): GroupMeta {
	const down = monitors.filter((m) => m.status === 'down').length;
	if (down > 0) return { text: `${down} ${down === 1 ? 'caído' : 'caídos'}`, tone: 'bad' };
	const degraded = monitors.filter((m) => m.status === 'degraded').length;
	if (degraded > 0) return { text: `${degraded} ${degraded === 1 ? 'degradado' : 'degradados'}`, tone: 'warn' };
	// Only pending monitors: nothing to call operational yet.
	if (worstStatus(monitors) === 'none') return { text: 'Sin verificar', tone: 'plain' };
	return { text: 'Operativo', tone: 'plain' };
}

export interface ChartCard {
	key: string;
	/** null is the "Sin ambiente" card. */
	environment: Environment | null;
	/** Fronts first, then APIs, each by name. */
	monitors: Monitor[];
}

export interface ChartGroup {
	key: string;
	/** null groups the monitors without a service ("Sin servicio"). */
	service: string | null;
	worst: WorstStatus;
	meta: GroupMeta;
	cards: ChartCard[];
}

/** One group per service (alphabetical, "Sin servicio" last) and one card per
 * environment inside it: prod, prepro, qa, dev, then the monitors without one. */
export function buildChartGroups(monitors: Monitor[]): ChartGroup[] {
	return groupByService(monitors).map((group) => {
		const cards: ChartCard[] = [];
		// groupByService already sorts by environment rank, then front
		// before API, then name, so first appearance gives the card order.
		for (const m of group.monitors) {
			const card = cards.find((c) => c.environment === m.environment);
			if (card) card.monitors.push(m);
			else
				cards.push({
					key: `${group.service ?? ''}|${m.environment ?? ''}`,
					environment: m.environment,
					monitors: [m]
				});
		}
		return {
			key: group.service ?? '',
			service: group.service,
			worst: worstStatus(group.monitors),
			meta: groupMeta(group.monitors),
			cards
		};
	});
}

// ---------- Series ----------

export interface Series {
	id: string;
	kind: MonitorKind;
	/** "Front" / "API", or the monitor's name when the card has more than one
	 * monitor of its kind, since color only tells kinds apart. */
	label: string;
	/** null draws a solid line; otherwise an SVG stroke-dasharray, for the
	 * second and later monitors of the same kind. */
	dash: string | null;
}

// Round caps add half the 2px stroke to each end of a dash, so a 6px gap
// shows as 4px; '0 5' is a dotted line.
const DASHES: (string | null)[] = [null, '6 6', '0 5'];

/** One line per monitor, fronts first (drawn first, so under the APIs). */
export function buildSeries(monitors: Monitor[]): Series[] {
	const sorted = monitors.slice().sort((a, b) => (a.kind === b.kind ? 0 : a.kind === 'front' ? -1 : 1));
	const perKind: Record<MonitorKind, number> = { front: 0, api: 0 };
	for (const m of sorted) perKind[m.kind]++;
	const seen: Record<MonitorKind, number> = { front: 0, api: 0 };
	return sorted.map((m) => {
		const index = seen[m.kind]++;
		return {
			id: m.id,
			kind: m.kind,
			label: perKind[m.kind] > 1 ? m.name : KIND_LABELS[m.kind],
			dash: DASHES[Math.min(index, DASHES.length - 1)]
		};
	});
}

// ---------- Geometry ----------
// Positions are percentages of the plot (x from the left, y from the top),
// shared by the SVG (viewBox 0 0 100 100) and the HTML overlays on it.

/** Smallest axis top of 50, 100, 250, 500, 1000, 2500, 5000, 10000 ms (and
 * on in the same 1-2.5-5 steps) that leaves 10 % headroom over the peak. */
export function niceMax(peak: number): number {
	const target = peak * 1.1;
	if (!Number.isFinite(target) || target <= 50) return 50;
	for (let scale = 100; ; scale *= 10) {
		for (const step of [0.5, 1, 2.5]) {
			if (step * scale >= target) return step * scale;
		}
	}
}

/** The highest value across every series, 0 when there is none. */
export function peakOf(values: (number | null)[][]): number {
	let peak = 0;
	for (const series of values) {
		for (const v of series) {
			if (v !== null && v > peak) peak = v;
		}
	}
	return peak;
}

/** The first bucket sits on the left edge and the last one on the right,
 * right under "Ahora" and next to the end labels. */
export function xOf(index: number, buckets: number): number {
	return buckets <= 1 ? 100 : (index / (buckets - 1)) * 100;
}

export function yOf(value: number, max: number): number {
	return 100 - (value / max) * 100;
}

export interface PlotPoint {
	x: number;
	y: number;
}

/** The line's unbroken runs. A null value (no successful check) ends a run
 * instead of drawing down to zero; a run of one point has no neighbor to
 * join, so the caller draws it as a dot. */
export function lineRuns(values: (number | null)[], max: number, buckets = values.length): PlotPoint[][] {
	const runs: PlotPoint[][] = [];
	let run: PlotPoint[] = [];
	values.forEach((v, i) => {
		if (v === null) {
			if (run.length > 0) runs.push(run);
			run = [];
		} else {
			run.push({ x: xOf(i, buckets), y: yOf(v, max) });
		}
	});
	if (run.length > 0) runs.push(run);
	return runs;
}

/** SVG polyline points for a run. */
export function polylinePoints(run: PlotPoint[]): string {
	return run.map((p) => `${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' ');
}

/** The last non-null value, where the line's end label goes. */
export function lastValue(values: (number | null)[]): number | null {
	for (let i = values.length - 1; i >= 0; i--) {
		if (values[i] !== null) return values[i];
	}
	return null;
}

/** Spreads label positions (in plot percent) so no two are closer than gap:
 * labels that would collide are merged into a cluster, spaced gap apart
 * around the cluster's mean, and kept inside [lo, hi]. When the labels cannot
 * all fit at gap (many series in one card) the gap shrinks evenly to the
 * range; staying inside [lo, hi] wins over the gap. Returns positions in the
 * input's order. */
export function spreadLabels(ys: number[], requestedGap: number, lo = 0, hi = 100): number[] {
	const gap = ys.length > 1 ? Math.min(requestedGap, (hi - lo) / (ys.length - 1)) : requestedGap;
	const order = ys.map((y, i) => ({ y, i })).sort((a, b) => a.y - b.y);
	let clusters = order.map((item) => ({ items: [item], center: item.y }));
	const extent = (c: { items: unknown[] }) => ((c.items.length - 1) * gap) / 2;

	let merged = true;
	while (merged) {
		merged = false;
		for (let k = 0; k < clusters.length - 1; k++) {
			const a = clusters[k];
			const b = clusters[k + 1];
			if (b.center - extent(b) - (a.center + extent(a)) < gap) {
				const items = [...a.items, ...b.items];
				const center = items.reduce((sum, item) => sum + item.y, 0) / items.length;
				clusters = [...clusters.slice(0, k), { items, center }, ...clusters.slice(k + 2)];
				merged = true;
				break;
			}
		}
	}

	const out = new Array<number>(ys.length);
	let floor = lo;
	for (const c of clusters) {
		const span = (c.items.length - 1) * gap;
		// Clamping can push a cluster into the previous one: floor keeps
		// the gap across clusters too.
		const top = Math.max(floor, Math.min(hi - span, c.center - span / 2));
		c.items.forEach((item, j) => (out[item.i] = top + j * gap));
		floor = top + span + gap;
	}
	// The floor above can push the last clusters past hi: pull them back,
	// keeping the gap. They fit, since gap * (n - 1) <= hi - lo.
	let cap = hi;
	for (let k = order.length - 1; k >= 0; k--) {
		const i = order[k].i;
		out[i] = Math.min(out[i], cap);
		cap = out[i] - gap;
	}
	return out;
}

// ---------- Text ----------

function seconds(ms: number, decimals: number): string {
	return (ms / 1000).toFixed(decimals).replace(/\.?0+$/, '').replace('.', ',');
}

/** "120 ms", or seconds from 1000 ms on: "1,2 s", "2 s". */
export function formatMs(ms: number): string {
	const rounded = Math.round(ms);
	return rounded < 1000 ? `${rounded} ms` : `${seconds(rounded, 1)} s`;
}

/** Axis labels: like formatMs, but with two decimals so half of 2500 reads
 * "1,25 s" instead of a rounded "1,3 s" that would sit on the wrong line. */
export function formatAxisMs(ms: number): string {
	const rounded = Math.round(ms);
	return rounded < 1000 ? `${rounded} ms` : `${seconds(rounded, 2)} s`;
}

const DAYS_ES = ['dom', 'lun', 'mar', 'mié', 'jue', 'vie', 'sáb'];

function pad(n: number): string {
	return String(n).padStart(2, '0');
}

/** A bucket's start in the browser's time zone: "14:15" for 24 h,
 * "lun 14:00" for 7 days, "5 nov 12:00" for 30 days. */
export function formatBucketTime(date: Date, range: LatencyRange): string {
	const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
	if (range === '24h') return time;
	if (range === '7d') return `${DAYS_ES[date.getDay()]} ${time}`;
	return `${date.getDate()} ${MONTHS_ES[date.getMonth()]} ${time}`;
}

/** The start of bucket index, from the response's window. */
export function bucketDate(from: string, bucketMinutes: number, index: number): Date {
	return new Date(Date.parse(from) + index * bucketMinutes * 60000);
}

/** A series' value in the tooltip. A bucket without a successful check is
 * "Sin respuesta" when checks ran and failed, "Sin datos" when none ran. */
export function pointLabel(point: LatencyPoint | undefined): string {
	if (!point) return 'Sin datos';
	if (point.avgMs !== null) return `${formatMs(point.avgMs)}${point.status === 'degraded' ? ' · degradada' : ''}`;
	return point.checks > 0 ? 'Sin respuesta' : 'Sin datos';
}

function joinEs(parts: string[]): string {
	if (parts.length <= 1) return parts.join('');
	return `${parts.slice(0, -1).join(', ')} y ${parts[parts.length - 1]}`;
}

/** "Latencia de Front y API de Pagos en Prod, promedio cada 15 minutos,
 * últimas 24 horas". */
export function chartAriaLabel(
	service: string | null,
	environment: Environment | null,
	series: Series[],
	range: LatencyRange
): string {
	const where = `${service ? ` de ${service}` : ', sin servicio'}${environment ? ` en ${ENV_LABELS[environment]}` : ', sin ambiente'}`;
	return `Latencia de ${joinEs(series.map((s) => s.label))}${where}, ${RANGE_INFO[range].caption.toLowerCase()}`;
}

// ---------- Table ----------

export interface SeriesSummary {
	/** Mean of the buckets' averages, null without any successful check. */
	avgMs: number | null;
	maxMs: number | null;
	/** Buckets whose worst status is down. */
	downBuckets: number;
	/** Buckets with at least one check. */
	checkedBuckets: number;
}

export function summarize(points: LatencyPoint[]): SeriesSummary {
	let sum = 0;
	let count = 0;
	let maxMs: number | null = null;
	let downBuckets = 0;
	let checkedBuckets = 0;
	for (const p of points) {
		if (p.avgMs !== null) {
			sum += p.avgMs;
			count++;
		}
		if (p.maxMs !== null && (maxMs === null || p.maxMs > maxMs)) maxMs = p.maxMs;
		if (p.checks > 0) checkedBuckets++;
		if (p.status === 'down') downBuckets++;
	}
	return { avgMs: count > 0 ? sum / count : null, maxMs, downBuckets, checkedBuckets };
}

/** "Intervalos con caída": "3 de 96", "Sin datos" when no bucket had checks. */
export function downBucketsLabel(summary: SeriesSummary): string {
	return summary.checkedBuckets === 0 ? 'Sin datos' : `${summary.downBuckets} de ${summary.checkedBuckets}`;
}

/** The card's one-line summary: "Promedio: Front 212 ms · API sin datos". */
export function cardSummary(series: Series[], summaries: SeriesSummary[]): string {
	const parts = series.map((s, i) => {
		const avg = summaries[i]?.avgMs ?? null;
		return `${s.label} ${avg === null ? 'sin datos' : formatMs(avg)}`;
	});
	return `Promedio: ${parts.join(' · ')}`;
}
