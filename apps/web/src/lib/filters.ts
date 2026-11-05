import type { Environment, Monitor } from './api';
import { ENV_LABELS, KIND_LABELS, filterByEnv, orderMonitors, serviceNames, type EnvFilter } from './grouping';

/** Stands for "Sin servicio" in a service selection, so monitors without a
 * service can be picked like any named service. */
export const NO_SERVICE = '';

export function serviceKey(m: Pick<Monitor, 'service'>): string {
	return m.service ?? NO_SERVICE;
}

export function serviceLabel(key: string): string {
	return key === NO_SERVICE ? 'Sin servicio' : key;
}

/** An empty selection means every service. */
export function filterByServices(monitors: Monitor[], services: string[]): Monitor[] {
	if (services.length === 0) return monitors;
	return monitors.filter((m) => services.includes(serviceKey(m)));
}

/** What the dashboard shows: the selected environment, then the selected
 * services. */
export function filterMonitors(monitors: Monitor[], env: EnvFilter, services: string[]): Monitor[] {
	return filterByServices(filterByEnv(monitors, env), services);
}

/** Reads a stored selection; anything that is not an array of strings is
 * treated as "every service". */
export function parseServices(raw: string | null): string[] {
	if (raw === null) return [];
	try {
		const value: unknown = JSON.parse(raw);
		if (!Array.isArray(value) || !value.every((s) => typeof s === 'string')) return [];
		return [...new Set(value as string[])];
	} catch {
		return [];
	}
}

export function toggleService(services: string[], key: string): string[] {
	return services.includes(key) ? services.filter((s) => s !== key) : [...services, key];
}

/** Drops selected services that no monitor has anymore, in any environment.
 * Returns the same array when nothing is dropped, so a caller can tell a
 * no-op by reference and not write state (or storage) again. Only call it
 * with a loaded monitor list: against the empty list before the first load
 * it would drop the whole saved selection. */
export function pruneServices(services: string[], monitors: Monitor[]): string[] {
	const existing = new Set(monitors.map(serviceKey));
	const kept = services.filter((s) => existing.has(s));
	return kept.length === services.length ? services : kept;
}

export type WorstStatus = 'down' | 'degraded' | 'up' | 'none';

/** The status a group of monitors shows as one dot. Pending-only or empty
 * groups have nothing to report yet ('none'). */
export function worstStatus(monitors: Monitor[]): WorstStatus {
	if (monitors.some((m) => m.status === 'down')) return 'down';
	if (monitors.some((m) => m.status === 'degraded')) return 'degraded';
	return monitors.some((m) => m.status === 'up') ? 'up' : 'none';
}

export const WORST_DOT: Record<WorstStatus, string> = {
	down: '#DF4730',
	degraded: '#FFC700',
	up: '#289B4F',
	none: 'var(--m-none)'
};

export interface ServiceFilterItem {
	key: string;
	name: string;
	/** Monitors of the service in the selected environment. */
	count: number;
	down: number;
	degraded: number;
	worst: WorstStatus;
	checked: boolean;
}

/** One entry per service that exists in any environment (alphabetical),
 * plus "Sin servicio" when some monitor has no service. Counts and status
 * only look at the selected environment, so a service can be listed with 0. */
export function serviceFilterItems(monitors: Monitor[], env: EnvFilter, services: string[]): ServiceFilterItem[] {
	const inEnv = filterByEnv(monitors, env);
	const keys = serviceNames(monitors);
	if (monitors.some((m) => m.service === null)) keys.push(NO_SERVICE);
	return keys.map((key) => {
		const list = inEnv.filter((m) => serviceKey(m) === key);
		return {
			key,
			name: serviceLabel(key),
			count: list.length,
			down: list.filter((m) => m.status === 'down').length,
			degraded: list.filter((m) => m.status === 'degraded').length,
			worst: worstStatus(list),
			checked: services.includes(key)
		};
	});
}

/** The count on the right of a service entry: failures win over the plain
 * count, so a filter list alone already shows where something is wrong. */
export function serviceItemMeta(item: ServiceFilterItem): { text: string; tone: 'bad' | 'warn' | 'plain' } {
	if (item.down > 0) return { text: `${item.down} ${item.down === 1 ? 'caído' : 'caídos'}`, tone: 'bad' };
	if (item.degraded > 0) {
		return { text: `${item.degraded} ${item.degraded === 1 ? 'degradado' : 'degradados'}`, tone: 'warn' };
	}
	return { text: String(item.count), tone: 'plain' };
}

export function matchesServiceQuery(item: ServiceFilterItem, query: string): boolean {
	const q = query.trim().toLocaleLowerCase('es');
	return q === '' || item.name.toLocaleLowerCase('es').includes(q);
}

/** The compact picker's button: "Todos los servicios" / "1 servicio" / "3 servicios". */
export function pickerLabel(services: string[]): string {
	if (services.length === 0) return 'Todos los servicios';
	return `${services.length} ${services.length === 1 ? 'servicio' : 'servicios'}`;
}

/** "Pagos, Sin servicio · Prod" over the wide list. */
export function scopeTitle(env: EnvFilter, services: string[]): string {
	const servicesPart = services.length === 0 ? 'Todos los servicios' : services.map(serviceLabel).join(', ');
	return `${servicesPart} · ${env === 'all' ? 'Todos los ambientes' : ENV_LABELS[env]}`;
}

export interface FilterState {
	env: EnvFilter;
	services: string[];
}

/** Brings a monitor hidden by the filters into view without dropping what
 * is already selected: its service joins the selection (unless every
 * service is already shown) and, if the environment filter hides it, the
 * filter widens to every environment. */
export function revealMonitor(state: FilterState, m: Pick<Monitor, 'service' | 'environment'>): FilterState {
	const key = serviceKey(m);
	let { env, services } = state;
	if (services.length > 0 && !services.includes(key)) services = [...services, key];
	if (env !== 'all' && m.environment !== env) env = 'all';
	return { env, services };
}

export interface OutsideItem {
	id: string;
	/** "{service} · Front|API", or the monitor's name when it has no service. */
	label: string;
	/** " · Dev|QA|Prepro|Prod" whenever the monitor has an environment: the filter
	 * that hid it may be the environment itself. */
	env: string;
	status: 'down' | 'degraded';
	service: string | null;
	environment: Environment | null;
}

export interface OutsideStrip {
	text: string;
	items: OutsideItem[];
}

/** The failures the filters hide (down first, then degraded, each in list
 * order), so a filter never hides an outage silently; null when there are
 * none. */
export function buildOutside(all: Monitor[], visible: Monitor[]): OutsideStrip | null {
	const shown = new Set(visible.map((m) => m.id));
	const hidden = orderMonitors(all.filter((m) => !shown.has(m.id)));
	const down = hidden.filter((m) => m.status === 'down');
	const degraded = hidden.filter((m) => m.status === 'degraded');
	if (down.length === 0 && degraded.length === 0) return null;

	const parts: string[] = [];
	if (down.length > 0) parts.push(`${down.length} sin respuesta`);
	if (degraded.length > 0) parts.push(`${degraded.length} ${degraded.length === 1 ? 'degradado' : 'degradados'}`);

	return {
		text: `Fuera de este filtro: ${parts.join(' · ')}`,
		items: [...down, ...degraded].map((m) => ({
			id: m.id,
			label: m.service ? `${m.service} · ${KIND_LABELS[m.kind]}` : m.name,
			env: m.environment ? ` · ${ENV_LABELS[m.environment]}` : '',
			status: m.status as 'down' | 'degraded',
			service: m.service,
			environment: m.environment
		}))
	};
}

export interface EmptyStateCopy {
	title: string;
	sub: string;
	/** Whether a "Ver todos" button that clears both filters makes sense. */
	canClear: boolean;
}

/** The list/board empty state, which depends on why nothing is visible. */
export function emptyStateCopy(total: number, env: EnvFilter, services: string[]): EmptyStateCopy {
	if (total === 0 || (services.length === 0 && env === 'all')) {
		return {
			title: 'Todavía no hay monitores',
			sub: 'Agrega la URL de un front o el endpoint de health de una API para empezar.',
			canClear: false
		};
	}
	if (services.length === 0 && env !== 'all') {
		return {
			title: `No hay monitores en ${ENV_LABELS[env]}`,
			sub: `Elige otro ambiente o agrega un monitor en ${ENV_LABELS[env]}.`,
			canClear: false
		};
	}
	const who =
		services.length > 1
			? 'de los servicios elegidos'
			: services[0] === NO_SERVICE
				? 'sin servicio'
				: `de ${services[0]}`;
	return {
		title: `No hay monitores ${who}${env === 'all' ? '' : ` en ${ENV_LABELS[env]}`}`,
		sub: 'Elige otro ambiente u otros servicios, o agrega un monitor.',
		canClear: true
	};
}
