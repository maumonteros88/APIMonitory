import type { Monitor } from './api';
import { countLabel } from './grouping';

// A company is an optional label that owns services. It is a grouping level
// above the service, not a filter: the filter selection still holds services
// only.

function compareNames(a: string, b: string): number {
	return a.localeCompare(b, 'es');
}

/** Distinct companies in use, for the datalists. */
export function companyNames(monitors: Monitor[]): string[] {
	const names = new Set<string>();
	for (const m of monitors) {
		if (m.company) names.add(m.company);
	}
	return [...names].sort(compareNames);
}

/** True when any monitor has a company. While false, every view renders
 * exactly as it did before companies existed. */
export function hasCompanies(monitors: Monitor[]): boolean {
	return monitors.some((m) => m.company !== null);
}

/** The most frequent company of a service's monitors; a tie goes to the
 * alphabetically first. Null when none of them has one. */
function mostFrequent(counts: Map<string, number>): string | null {
	let best: string | null = null;
	let bestCount = 0;
	for (const [company, count] of counts) {
		if (count > bestCount || (count === bestCount && best !== null && compareNames(company, best) < 0)) {
			best = company;
			bestCount = count;
		}
	}
	return best;
}

/** The company a service belongs to. This is the single rule deciding where
 * a service is shown and what a form prefills, so a service whose monitors
 * disagree still lands in exactly one place. */
export function serviceCompany(monitors: Monitor[], service: string): string | null {
	const counts = new Map<string, number>();
	for (const m of monitors) {
		if (m.service === service && m.company) counts.set(m.company, (counts.get(m.company) ?? 0) + 1);
	}
	return mostFrequent(counts);
}

/** serviceCompany for every service at once. Build it from the full monitor
 * list, never from a filtered one: a narrow environment filter must not move
 * a service to another company. */
export interface CompanyIndex {
	/** hasCompanies of the list it was built from. */
	enabled: boolean;
	/** Only services that have a company. */
	byService: ReadonlyMap<string, string>;
}

export const NO_COMPANIES: CompanyIndex = { enabled: false, byService: new Map() };

export function buildCompanyIndex(monitors: Monitor[]): CompanyIndex {
	const perService = new Map<string, Map<string, number>>();
	for (const m of monitors) {
		if (m.service === null || !m.company) continue;
		let counts = perService.get(m.service);
		if (!counts) {
			counts = new Map();
			perService.set(m.service, counts);
		}
		counts.set(m.company, (counts.get(m.company) ?? 0) + 1);
	}
	const byService = new Map<string, string>();
	for (const [service, counts] of perService) {
		const company = mostFrequent(counts);
		if (company !== null) byService.set(service, company);
	}
	return { enabled: hasCompanies(monitors), byService };
}

export interface CompanyBlock<T> {
	/** null is "Sin empresa". */
	company: string | null;
	items: T[];
}

/** Splits already ordered items into one block per company: companies
 * alphabetically, "Sin empresa" (null) last, and the original order kept
 * inside each block. */
export function partitionByCompany<T>(items: T[], companyOf: (item: T) => string | null): CompanyBlock<T>[] {
	const byCompany = new Map<string | null, T[]>();
	for (const item of items) {
		const company = companyOf(item);
		const list = byCompany.get(company);
		if (list) list.push(item);
		else byCompany.set(company, [item]);
	}
	return [...byCompany.keys()]
		.sort((a, b) => (a === null ? 1 : b === null ? -1 : compareNames(a, b)))
		.map((company) => ({ company, items: byCompany.get(company)! }));
}

/** "3 servicios · 8 monitores". */
export function companyMeta(services: number, monitors: number): string {
	return `${services} ${services === 1 ? 'servicio' : 'servicios'} · ${countLabel(monitors)}`;
}

export interface CompanySection<T> extends CompanyBlock<T> {
	meta: string;
}

export interface CompanyLayout<T> {
	sections: CompanySection<T>[];
	/** The items without a service, kept out of the company sections. */
	noService: T[];
}

/** Arranges service groups (list groups, board cards, matrix rows, chart
 * groups) into company sections. Items without a service stay out of the
 * sections whatever their monitors' company is. monitorCount says how many
 * monitors an item holds, for each section's meta. */
export function groupByCompany<T extends { service: string | null }>(
	items: T[],
	index: CompanyIndex,
	monitorCount: (item: T) => number
): CompanyLayout<T> {
	const withService = items.filter((item) => item.service !== null);
	const sections = partitionByCompany(withService, (item) => index.byService.get(item.service!) ?? null).map(
		(block) => ({
			...block,
			meta: companyMeta(
				block.items.length,
				block.items.reduce((sum, item) => sum + monitorCount(item), 0)
			)
		})
	);
	return { sections, noService: items.filter((item) => item.service === null) };
}

export type CompanySelection = 'all' | 'some' | 'none';

/** How many of a company's services are selected: all, some or none. A
 * company with no services is 'none'. */
export function companySelection(selected: string[], companyServiceKeys: string[]): CompanySelection {
	if (companyServiceKeys.length === 0) return 'none';
	const count = companyServiceKeys.filter((key) => selected.includes(key)).length;
	if (count === 0) return 'none';
	return count === companyServiceKeys.length ? 'all' : 'some';
}

/** The company checkbox: unselects its services when every one is selected,
 * otherwise selects the missing ones (appended, like toggleService). */
export function toggleCompany(selected: string[], companyServiceKeys: string[]): string[] {
	if (companyServiceKeys.length === 0) return selected;
	if (companySelection(selected, companyServiceKeys) === 'all') {
		return selected.filter((key) => !companyServiceKeys.includes(key));
	}
	return [...selected, ...companyServiceKeys.filter((key) => !selected.includes(key))];
}

/** The form's Empresa field. auto is true while its value is one the form
 * filled in itself. */
export interface CompanyField {
	value: string;
	auto: boolean;
}

/** What the Empresa field holds after the Servicio field changed. matched is
 * the company of the existing service the Servicio value names, or null.
 * With a match, the field is filled only when it is empty or still holds an
 * auto-filled value: what the user typed is never overwritten. Without one,
 * an auto-filled value is dropped (the service it came from is no longer
 * named) and a typed one stays. */
export function prefillCompany(field: CompanyField, matched: string | null): CompanyField {
	if (matched !== null) {
		return field.value.trim() === '' || field.auto ? { value: matched, auto: true } : field;
	}
	return field.auto ? { value: '', auto: false } : field;
}

/** The monitors after a company was assigned to a whole service. */
export function applyServiceCompany(monitors: Monitor[], service: string, company: string | null): Monitor[] {
	return monitors.map((m) => (m.service === service ? { ...m, company } : m));
}

/** Marks the "Asignar empresa" buttons, so focus can come back to one after
 * the list re-renders. */
export const ASSIGN_COMPANY_ATTR = 'data-assign-company';
