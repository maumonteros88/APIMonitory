import { describe, expect, it } from 'vitest';
import type { Environment, Monitor, MonitorKind } from './api';
import {
	applyServiceCompany,
	buildCompanyIndex,
	companyMeta,
	companyNames,
	companySelection,
	groupByCompany,
	hasCompanies,
	partitionByCompany,
	prefillCompany,
	serviceCompany,
	toggleCompany
} from './companies';
import { groupByService } from './grouping';

function makeMonitor(
	id: string,
	service: string | null,
	company: string | null,
	kind: MonitorKind = 'api',
	environment: Environment | null = 'prod'
): Monitor {
	return {
		id,
		name: id,
		description: '',
		url: 'https://example.com',
		service,
		company,
		kind,
		environment,
		status: 'up',
		checking: false,
		lastCheck: null,
		commit: null,
		uptimePercent: 100,
		history: []
	};
}

describe('companyNames', () => {
	it('lists the distinct companies sorted for Spanish, without nulls', () => {
		const monitors = [
			makeMonitor('a', 'Pagos', 'Empresa B'),
			makeMonitor('b', 'Pagos', 'Empresa B'),
			makeMonitor('c', 'Backoffice', 'Empresa A'),
			makeMonitor('d', 'Ejemplo', null),
			makeMonitor('e', null, 'Épsilon')
		];
		expect(companyNames(monitors)).toEqual(['Empresa A', 'Empresa B', 'Épsilon']);
	});

	it('is empty without companies', () => {
		expect(companyNames([makeMonitor('a', 'Pagos', null)])).toEqual([]);
	});
});

describe('hasCompanies', () => {
	it('is true when any monitor has a company, even one without a service', () => {
		expect(hasCompanies([makeMonitor('a', 'Pagos', null), makeMonitor('b', null, 'Empresa A')])).toBe(true);
	});

	it('is false when none has one', () => {
		expect(hasCompanies([makeMonitor('a', 'Pagos', null)])).toBe(false);
		expect(hasCompanies([])).toBe(false);
	});
});

describe('serviceCompany', () => {
	it('is the company of the service', () => {
		const monitors = [makeMonitor('a', 'Pagos', 'Empresa A'), makeMonitor('b', 'Backoffice', 'Empresa B')];
		expect(serviceCompany(monitors, 'Pagos')).toBe('Empresa A');
	});

	it('takes the most frequent company when the monitors disagree', () => {
		const monitors = [
			makeMonitor('a', 'Pagos', 'Empresa A'),
			makeMonitor('b', 'Pagos', 'Empresa B'),
			makeMonitor('c', 'Pagos', 'Empresa B')
		];
		expect(serviceCompany(monitors, 'Pagos')).toBe('Empresa B');
	});

	it('breaks a tie alphabetically', () => {
		const monitors = [makeMonitor('a', 'Pagos', 'Empresa B'), makeMonitor('b', 'Pagos', 'Empresa A')];
		expect(serviceCompany(monitors, 'Pagos')).toBe('Empresa A');
	});

	it('ignores monitors without a company when counting', () => {
		const monitors = [
			makeMonitor('a', 'Pagos', null),
			makeMonitor('b', 'Pagos', null),
			makeMonitor('c', 'Pagos', 'Empresa A')
		];
		expect(serviceCompany(monitors, 'Pagos')).toBe('Empresa A');
	});

	it('is null when no monitor of the service has one, or the service is unknown', () => {
		const monitors = [makeMonitor('a', 'Pagos', null), makeMonitor('b', 'Backoffice', 'Empresa A')];
		expect(serviceCompany(monitors, 'Pagos')).toBeNull();
		expect(serviceCompany(monitors, 'Otro')).toBeNull();
	});
});

describe('buildCompanyIndex', () => {
	const monitors = [
		makeMonitor('a', 'Pagos', 'Empresa A'),
		makeMonitor('b', 'Pagos', 'Empresa B'),
		makeMonitor('c', 'Pagos', 'Empresa B'),
		makeMonitor('d', 'Backoffice', null),
		makeMonitor('e', null, 'Empresa A')
	];

	it('applies the serviceCompany rule to every service', () => {
		const index = buildCompanyIndex(monitors);
		expect([...index.byService]).toEqual([['Pagos', 'Empresa B']]);
		for (const service of ['Pagos', 'Backoffice']) {
			expect(index.byService.get(service) ?? null).toBe(serviceCompany(monitors, service));
		}
	});

	it('is enabled exactly when hasCompanies is', () => {
		expect(buildCompanyIndex(monitors).enabled).toBe(true);
		expect(buildCompanyIndex([makeMonitor('a', 'Pagos', null)]).enabled).toBe(false);
	});
});

describe('partitionByCompany', () => {
	it('orders companies alphabetically with null last and keeps the order inside', () => {
		const companyOf: Record<string, string | null> = { z: 'Empresa B', y: null, x: 'Empresa A', w: 'Empresa B' };
		const blocks = partitionByCompany(['z', 'y', 'x', 'w'], (item) => companyOf[item]);
		expect(blocks).toEqual([
			{ company: 'Empresa A', items: ['x'] },
			{ company: 'Empresa B', items: ['z', 'w'] },
			{ company: null, items: ['y'] }
		]);
	});
});

describe('companyMeta', () => {
	it('counts services and monitors', () => {
		expect(companyMeta(3, 8)).toBe('3 servicios · 8 monitores');
		expect(companyMeta(1, 1)).toBe('1 servicio · 1 monitor');
	});
});

describe('groupByCompany', () => {
	const monitors = [
		makeMonitor('pagos-api', 'Pagos', 'Empresa B'),
		makeMonitor('pagos-front', 'Pagos', 'Empresa B', 'front'),
		makeMonitor('bo-api', 'Backoffice', 'Empresa A'),
		makeMonitor('ej-api', 'Ejemplo', null),
		makeMonitor('reportes-api', 'Reportes', 'Empresa A'),
		makeMonitor('loose', null, 'Empresa A')
	];
	const groups = groupByService(monitors);
	const index = buildCompanyIndex(monitors);
	const layout = groupByCompany(groups, index, (g) => g.monitors.length);

	it('sections by company: alphabetical, then "Sin empresa"; services keep their order', () => {
		expect(layout.sections.map((s) => [s.company, s.items.map((g) => g.service)])).toEqual([
			['Empresa A', ['Backoffice', 'Reportes']],
			['Empresa B', ['Pagos']],
			[null, ['Ejemplo']]
		]);
	});

	it('describes each section by its services and monitors', () => {
		expect(layout.sections.map((s) => s.meta)).toEqual([
			'2 servicios · 2 monitores',
			'1 servicio · 2 monitores',
			'1 servicio · 1 monitor'
		]);
	});

	it('keeps monitors without a service out of the sections, whatever their company', () => {
		expect(layout.noService.map((g) => g.service)).toEqual([null]);
		expect(layout.sections.flatMap((s) => s.items).some((g) => g.service === null)).toBe(false);
	});

	it('places a service whose monitors disagree in one section only', () => {
		const split = [makeMonitor('a', 'Pagos', 'Empresa A'), makeMonitor('b', 'Pagos', 'Empresa B'), makeMonitor('c', 'Pagos', 'Empresa B')];
		const result = groupByCompany(groupByService(split), buildCompanyIndex(split), (g) => g.monitors.length);
		expect(result.sections.map((s) => [s.company, s.items.length])).toEqual([['Empresa B', 1]]);
	});

	it('uses the full-list index, so a narrower monitor list cannot move a service', () => {
		const split = [makeMonitor('a', 'Pagos', 'Empresa A'), makeMonitor('b', 'Pagos', 'Empresa B'), makeMonitor('c', 'Pagos', 'Empresa B')];
		const index = buildCompanyIndex(split);
		const onlyA = groupByService(split.filter((m) => m.id === 'a'));
		expect(groupByCompany(onlyA, index, (g) => g.monitors.length).sections[0].company).toBe('Empresa B');
	});
});

describe('companySelection and toggleCompany', () => {
	const keys = ['Pagos', 'Backoffice'];

	it('reports all / some / none of the services selected', () => {
		expect(companySelection(['Pagos', 'Backoffice', 'Otro'], keys)).toBe('all');
		expect(companySelection(['Pagos'], keys)).toBe('some');
		expect(companySelection(['Otro'], keys)).toBe('none');
		expect(companySelection([], keys)).toBe('none');
		expect(companySelection(['Pagos'], [])).toBe('none');
	});

	it('selects the missing services after the existing selection', () => {
		expect(toggleCompany(['Otro'], keys)).toEqual(['Otro', 'Pagos', 'Backoffice']);
		expect(toggleCompany(['Backoffice', 'Otro'], keys)).toEqual(['Backoffice', 'Otro', 'Pagos']);
		expect(toggleCompany([], keys)).toEqual(['Pagos', 'Backoffice']);
	});

	it('unselects every service of the company when all are selected', () => {
		expect(toggleCompany(['Otro', 'Pagos', 'Backoffice'], keys)).toEqual(['Otro']);
	});

	it('does not mutate its input', () => {
		const selected = ['Pagos'];
		toggleCompany(selected, keys);
		expect(selected).toEqual(['Pagos']);
	});

	it('leaves the selection alone for a company without services', () => {
		expect(toggleCompany(['Pagos'], [])).toEqual(['Pagos']);
	});
});

describe('prefillCompany', () => {
	it('fills an empty field from the matched service', () => {
		expect(prefillCompany({ value: '', auto: false }, 'Empresa A')).toEqual({ value: 'Empresa A', auto: true });
		expect(prefillCompany({ value: '  ', auto: false }, 'Empresa A')).toEqual({ value: 'Empresa A', auto: true });
	});

	it('replaces a value the form filled in itself', () => {
		expect(prefillCompany({ value: 'Empresa A', auto: true }, 'Empresa B')).toEqual({ value: 'Empresa B', auto: true });
	});

	it('never overwrites what the user typed', () => {
		const typed = { value: 'Empresa C', auto: false };
		expect(prefillCompany(typed, 'Empresa A')).toBe(typed);
	});

	it('drops an auto-filled value when the service stops matching, but keeps a typed one', () => {
		expect(prefillCompany({ value: 'Empresa A', auto: true }, null)).toEqual({ value: '', auto: false });
		const typed = { value: 'Empresa C', auto: false };
		expect(prefillCompany(typed, null)).toBe(typed);
	});
});

describe('applyServiceCompany', () => {
	it('sets the company on every monitor of the service only', () => {
		const monitors = [makeMonitor('a', 'Pagos', null), makeMonitor('b', 'Pagos', 'Empresa A'), makeMonitor('c', 'Backoffice', null)];
		const result = applyServiceCompany(monitors, 'Pagos', 'Empresa B');
		expect(result.map((m) => m.company)).toEqual(['Empresa B', 'Empresa B', null]);
		expect(result[2]).toBe(monitors[2]);
	});

	it('clears it with null', () => {
		const monitors = [makeMonitor('a', 'Pagos', 'Empresa A')];
		expect(applyServiceCompany(monitors, 'Pagos', null)[0].company).toBeNull();
	});
});
