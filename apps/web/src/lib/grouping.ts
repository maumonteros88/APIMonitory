import type { Environment, Monitor, MonitorKind } from './api';

/** The dashboard's environment filter: one environment, or every monitor. */
export type EnvFilter = 'all' | Environment;

export const ENV_FILTERS: EnvFilter[] = ['all', 'dev', 'qa', 'prod'];

export const ENV_LABELS: Record<Environment, string> = { dev: 'Dev', qa: 'QA', prod: 'Prod' };

export const KIND_LABELS: Record<MonitorKind, string> = { front: 'Front', api: 'API' };

/** Board matrix columns, left to right. */
export const MATRIX_ENVS: Environment[] = ['dev', 'qa', 'prod'];

export function isEnvFilter(value: unknown): value is EnvFilter {
	return typeof value === 'string' && (ENV_FILTERS as string[]).includes(value);
}

/** "1 monitor" / "3 monitores". */
export function countLabel(n: number): string {
	return `${n} ${n === 1 ? 'monitor' : 'monitores'}`;
}

/** Monitors without an environment only show up under 'all'. */
export function filterByEnv(monitors: Monitor[], env: EnvFilter): Monitor[] {
	return env === 'all' ? monitors : monitors.filter((m) => m.environment === env);
}

export function envCounts(monitors: Monitor[]): Record<EnvFilter, number> {
	const counts: Record<EnvFilter, number> = { all: monitors.length, dev: 0, qa: 0, prod: 0 };
	for (const m of monitors) {
		if (m.environment) counts[m.environment]++;
	}
	return counts;
}

const ENV_RANK: Record<Environment, number> = { prod: 0, qa: 1, dev: 2 };

function envRank(m: Monitor): number {
	return m.environment ? ENV_RANK[m.environment] : 3;
}

function compareNames(a: string, b: string): number {
	return a.localeCompare(b, 'es');
}

/** Order of monitors inside a service: prod, qa, dev, then no environment;
 * fronts before APIs within an environment; then by name. */
export function compareMonitors(a: Monitor, b: Monitor): number {
	const byEnv = envRank(a) - envRank(b);
	if (byEnv !== 0) return byEnv;
	if (a.kind !== b.kind) return a.kind === 'front' ? -1 : 1;
	return compareNames(a.name, b.name);
}

export interface ServiceGroup {
	/** null groups the monitors without a service. */
	service: string | null;
	monitors: Monitor[];
}

/** Groups by service, alphabetically, with the monitors without a service
 * last; each group's monitors are sorted by compareMonitors. */
export function groupByService(monitors: Monitor[]): ServiceGroup[] {
	const byService = new Map<string | null, Monitor[]>();
	for (const m of monitors) {
		const list = byService.get(m.service);
		if (list) list.push(m);
		else byService.set(m.service, [m]);
	}
	return [...byService.keys()]
		.sort((a, b) => (a === null ? 1 : b === null ? -1 : compareNames(a, b)))
		.map((service) => ({ service, monitors: byService.get(service)!.slice().sort(compareMonitors) }));
}

/** The same order the list view shows, flattened. */
export function orderMonitors(monitors: Monitor[]): Monitor[] {
	return groupByService(monitors).flatMap((g) => g.monitors);
}

/** "Front y API" / "Solo front" / "Solo API". */
export function kindsLabel(monitors: Monitor[]): string {
	const hasFront = monitors.some((m) => m.kind === 'front');
	const hasApi = monitors.some((m) => m.kind === 'api');
	if (hasFront && hasApi) return 'Front y API';
	return hasFront ? 'Solo front' : 'Solo API';
}

/** List-view group header meta. */
export function groupMeta(group: ServiceGroup): string {
	const count = countLabel(group.monitors.length);
	return group.service === null ? count : `${count} · ${kindsLabel(group.monitors)}`;
}

/** Distinct services in use, for the form's suggestions. */
export function serviceNames(monitors: Monitor[]): string[] {
	const names = new Set<string>();
	for (const m of monitors) {
		if (m.service) names.add(m.service);
	}
	return [...names].sort(compareNames);
}

export interface AlertItem {
	id: string;
	/** "{service} · Front|API", or the monitor's name when it has no service. */
	label: string;
	/** " · Dev|QA|Prod" under the 'all' filter, "" otherwise. */
	env: string;
	/** The monitor's service, so a chip can focus the view on it. */
	service: string | null;
}

export interface Banner {
	level: 'bad' | 'warn' | 'ok' | 'none';
	text: string;
	items: AlertItem[];
}

function alertItem(m: Monitor, env: EnvFilter): AlertItem {
	return {
		id: m.id,
		label: m.service ? `${m.service} · ${KIND_LABELS[m.kind]}` : m.name,
		env: env === 'all' && m.environment ? ` · ${ENV_LABELS[m.environment]}` : '',
		service: m.service
	};
}

/** The status banners for the monitors visible under the filters: one per
 * bad state (down first, then degraded), or a single "all good" / "nothing
 * to show" banner. total is how many monitors exist at all, so an empty view
 * can tell "nothing configured" from "the filters hide everything". */
export function buildBanners(visible: Monitor[], env: EnvFilter, total = visible.length): Banner[] {
	if (visible.length === 0) {
		const text = total > 0 ? 'Sin monitores en este filtro' : 'Sin monitores configurados';
		return [{ level: 'none', text, items: [] }];
	}

	const ordered = orderMonitors(visible);
	const down = ordered.filter((m) => m.status === 'down');
	const degraded = ordered.filter((m) => m.status === 'degraded');
	const banners: Banner[] = [];
	if (down.length > 0) {
		banners.push({
			level: 'bad',
			text: `${countLabel(down.length)} sin respuesta`,
			items: down.map((m) => alertItem(m, env))
		});
	}
	if (degraded.length > 0) {
		banners.push({
			level: 'warn',
			text: `${countLabel(degraded.length)} con rendimiento degradado`,
			items: degraded.map((m) => alertItem(m, env))
		});
	}
	if (banners.length === 0) banners.push({ level: 'ok', text: 'Todo operativo', items: [] });
	return banners;
}

export function intervalLabel(seconds: number): string {
	return seconds >= 60 && seconds % 60 === 0 ? `${seconds / 60} min` : `${seconds} s`;
}

/** The line under the banners; "" when no monitor is visible. services is
 * the service selection ('' = no service, empty = every service). */
export function summaryLine(count: number, env: EnvFilter, intervalSeconds: number, services: string[] = []): string {
	if (count === 0) return '';
	const scope = env === 'all' ? 'todos los ambientes' : ENV_LABELS[env];
	const which =
		services.length === 0
			? ''
			: services.length > 1
				? ` de ${services.length} servicios`
				: services[0] === ''
					? ' sin servicio'
					: ` de ${services[0]}`;
	return `${countLabel(count)}${which} en ${scope} · se verifica cada ${intervalLabel(intervalSeconds)}`;
}

/** One board segment: a monitor, or a placeholder (monitor null) standing
 * for a kind the service has no monitor of. */
export interface Segment {
	kind: MonitorKind;
	monitor: Monitor | null;
}

/** Fronts first, then APIs, with a "Sin front" / "Sin API" placeholder when
 * one of the two kinds is missing. Empty for no monitors. */
export function buildSegments(monitors: Monitor[]): Segment[] {
	if (monitors.length === 0) return [];
	const sorted = monitors.slice().sort((a, b) => compareNames(a.name, b.name));
	const segmentsOf = (kind: MonitorKind): Segment[] => {
		const ofKind = sorted.filter((m) => m.kind === kind);
		return ofKind.length > 0 ? ofKind.map((monitor) => ({ kind, monitor })) : [{ kind, monitor: null }];
	};
	return [...segmentsOf('front'), ...segmentsOf('api')];
}

export interface BoardCard {
	service: string;
	meta: string;
	segments: Segment[];
}

export interface BoardLayout {
	cards: BoardCard[];
	/** Monitors without a service, in list order. */
	unassigned: Monitor[];
}

/** Board for a single environment: one card per service. */
export function buildBoard(visible: Monitor[]): BoardLayout {
	const cards: BoardCard[] = [];
	let unassigned: Monitor[] = [];
	for (const group of groupByService(visible)) {
		if (group.service === null) {
			unassigned = group.monitors;
			continue;
		}
		cards.push({
			service: group.service,
			meta: kindsLabel(group.monitors),
			segments: buildSegments(group.monitors)
		});
	}
	return { cards, unassigned };
}

export interface MatrixCell {
	env: Environment;
	/** Empty when the service has no monitor in env ("Sin monitores"). */
	segments: Segment[];
}

export interface MatrixRow {
	service: string;
	meta: string;
	cells: MatrixCell[];
}

export interface MatrixLayout {
	rows: MatrixRow[];
	/** Monitors missing a service or an environment, in list order. */
	unassigned: Monitor[];
}

/** Board under the 'all' filter: services by environment (MATRIX_ENVS). */
export function buildMatrix(monitors: Monitor[]): MatrixLayout {
	const placed = monitors.filter((m) => m.service !== null && m.environment !== null);
	const unassigned = orderMonitors(monitors.filter((m) => m.service === null || m.environment === null));
	const rows = groupByService(placed).map((group) => ({
		service: group.service!,
		meta: kindsLabel(group.monitors),
		cells: MATRIX_ENVS.map((env) => ({
			env,
			segments: buildSegments(group.monitors.filter((m) => m.environment === env))
		}))
	}));
	return { rows, unassigned };
}
