import { describe, expect, it } from 'vitest';
import type { Environment, Monitor, MonitorKind } from './api';
import {
	buildOutside,
	emptyStateCopy,
	filterByServices,
	filterMonitors,
	matchesServiceQuery,
	parseServices,
	pickerLabel,
	pruneServices,
	revealMonitor,
	scopeTitle,
	serviceFilterItems,
	serviceItemMeta,
	toggleService,
	worstStatus
} from './filters';

function makeMonitor(
	id: string,
	service: string | null,
	kind: MonitorKind,
	environment: Environment | null,
	status: Monitor['status'] = 'up'
): Monitor {
	return {
		id,
		name: id,
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

const ids = (monitors: Monitor[]) => monitors.map((m) => m.id);

const monitors = [
	makeMonitor('pagos-api-prod', 'Pagos', 'api', 'prod', 'down'),
	makeMonitor('pagos-api-qa', 'Pagos', 'api', 'qa'),
	makeMonitor('bo-front-prod', 'Backoffice', 'front', 'prod'),
	makeMonitor('bo-api-dev', 'Backoffice', 'api', 'dev', 'degraded'),
	makeMonitor('rep-api-qa', 'Reportes', 'api', 'qa', 'pending'),
	makeMonitor('loose', null, 'api', null)
];

describe('filterByServices and filterMonitors', () => {
	it('keeps everything for an empty selection', () => {
		expect(filterByServices(monitors, [])).toBe(monitors);
	});

	it('keeps the selected services, with "" standing for no service', () => {
		expect(ids(filterByServices(monitors, ['Pagos', '']))).toEqual(['pagos-api-prod', 'pagos-api-qa', 'loose']);
	});

	it('applies the environment first, then the services', () => {
		expect(ids(filterMonitors(monitors, 'prod', ['Pagos', 'Backoffice']))).toEqual(['pagos-api-prod', 'bo-front-prod']);
		expect(ids(filterMonitors(monitors, 'qa', ['']))).toEqual([]);
	});
});

describe('parseServices', () => {
	it('reads a stored JSON array of names, without duplicates', () => {
		expect(parseServices('["Pagos","","Pagos"]')).toEqual(['Pagos', '']);
	});

	it('treats anything else as every service', () => {
		for (const raw of [null, '', 'Pagos', '{"a":1}', '[1,2]', '["Pagos",null]']) expect(parseServices(raw)).toEqual([]);
	});
});

describe('toggleService', () => {
	it('adds a missing service at the end and removes a selected one', () => {
		expect(toggleService(['Pagos'], 'Backoffice')).toEqual(['Pagos', 'Backoffice']);
		expect(toggleService(['Pagos', 'Backoffice'], 'Pagos')).toEqual(['Backoffice']);
	});
});

describe('pruneServices', () => {
	it('drops names no monitor has anymore', () => {
		expect(pruneServices(['Pagos', 'Antiguo'], monitors)).toEqual(['Pagos']);
	});

	it('keeps a service that only exists in another environment', () => {
		const selection = ['Reportes'];
		expect(pruneServices(selection, filterMonitors(monitors, 'all', []))).toBe(selection);
	});

	it('keeps "Sin servicio" while some monitor has no service, and drops it otherwise', () => {
		expect(pruneServices([''], monitors)).toEqual(['']);
		expect(pruneServices(['', 'Pagos'], monitors.filter((m) => m.service !== null))).toEqual(['Pagos']);
	});

	it('returns the same array when nothing is dropped', () => {
		const selection = ['Pagos', 'Backoffice'];
		expect(pruneServices(selection, monitors)).toBe(selection);
	});

	it('can leave an empty selection, which means every service', () => {
		expect(pruneServices(['Antiguo'], monitors)).toEqual([]);
	});
});

describe('worstStatus', () => {
	it('ranks down over degraded over up, with nothing to report as none', () => {
		const [down, , up, degraded, pending] = monitors;
		expect(worstStatus([up, degraded, down])).toBe('down');
		expect(worstStatus([up, degraded])).toBe('degraded');
		expect(worstStatus([up, pending])).toBe('up');
		expect(worstStatus([pending])).toBe('none');
		expect(worstStatus([])).toBe('none');
	});
});

describe('serviceFilterItems and serviceItemMeta', () => {
	it('lists every service alphabetically plus "Sin servicio", counting only the selected environment', () => {
		const items = serviceFilterItems(monitors, 'prod', ['Pagos']);

		expect(items.map((i) => [i.key, i.name, i.count, i.worst, i.checked])).toEqual([
			['Backoffice', 'Backoffice', 1, 'up', false],
			['Pagos', 'Pagos', 1, 'down', true],
			['Reportes', 'Reportes', 0, 'none', false],
			['', 'Sin servicio', 0, 'none', false]
		]);
	});

	it('leaves out "Sin servicio" when every monitor has a service', () => {
		const items = serviceFilterItems(monitors.filter((m) => m.service !== null), 'all', []);
		expect(items.map((i) => i.key)).toEqual(['Backoffice', 'Pagos', 'Reportes']);
	});

	it('shows failures before the plain count', () => {
		const [backoffice, pagos, reportes] = serviceFilterItems(monitors, 'all', []);

		expect(serviceItemMeta(pagos)).toEqual({ text: '1 caído', tone: 'bad' });
		expect(serviceItemMeta(backoffice)).toEqual({ text: '1 degradado', tone: 'warn' });
		expect(serviceItemMeta(reportes)).toEqual({ text: '1', tone: 'plain' });
		expect(serviceItemMeta({ ...pagos, down: 3 })).toEqual({ text: '3 caídos', tone: 'bad' });
		expect(serviceItemMeta({ ...backoffice, degraded: 2 })).toEqual({ text: '2 degradados', tone: 'warn' });
	});

	it('matches a search case-insensitively', () => {
		const [backoffice] = serviceFilterItems(monitors, 'all', []);
		expect(matchesServiceQuery(backoffice, '  OFFI ')).toBe(true);
		expect(matchesServiceQuery(backoffice, 'pagos')).toBe(false);
		expect(matchesServiceQuery(backoffice, '')).toBe(true);
	});
});

describe('pickerLabel and scopeTitle', () => {
	it('labels the compact picker', () => {
		expect(pickerLabel([])).toBe('Todos los servicios');
		expect(pickerLabel(['Pagos'])).toBe('1 servicio');
		expect(pickerLabel(['Pagos', ''])).toBe('2 servicios');
	});

	it('names the services and the environment in view', () => {
		expect(scopeTitle('all', [])).toBe('Todos los servicios · Todos los ambientes');
		expect(scopeTitle('prod', ['Pagos', ''])).toBe('Pagos, Sin servicio · Prod');
	});
});

describe('revealMonitor', () => {
	const hiddenInQa = makeMonitor('pagos-api-qa', 'Pagos', 'api', 'qa', 'down');

	it('adds the service to the selection instead of replacing it', () => {
		expect(revealMonitor({ env: 'qa', services: ['Backoffice'] }, hiddenInQa)).toEqual({
			env: 'qa',
			services: ['Backoffice', 'Pagos']
		});
	});

	it('leaves the services alone when every service is already shown', () => {
		expect(revealMonitor({ env: 'prod', services: [] }, hiddenInQa)).toEqual({ env: 'all', services: [] });
	});

	it('widens the environment to all only when the environment filter hides the monitor', () => {
		expect(revealMonitor({ env: 'prod', services: ['Backoffice'] }, hiddenInQa)).toEqual({
			env: 'all',
			services: ['Backoffice', 'Pagos']
		});
		expect(revealMonitor({ env: 'all', services: ['Backoffice'] }, hiddenInQa).env).toBe('all');
	});

	it('does not duplicate a service already selected', () => {
		expect(revealMonitor({ env: 'prod', services: ['Pagos'] }, hiddenInQa)).toEqual({ env: 'all', services: ['Pagos'] });
	});

	it('adds "Sin servicio" for a monitor without a service, and leaves "all" for one without an environment', () => {
		const loose = makeMonitor('loose', null, 'api', null, 'down');
		expect(revealMonitor({ env: 'prod', services: ['Pagos'] }, loose)).toEqual({ env: 'all', services: ['Pagos', ''] });
	});
});

describe('buildOutside', () => {
	it('is null when the filters hide no failure', () => {
		expect(buildOutside(monitors, monitors)).toBeNull();
		expect(buildOutside(monitors, filterMonitors(monitors, 'all', ['Pagos', 'Backoffice']))).toBeNull();
	});

	it('lists the hidden down and degraded monitors, down first, with their environment', () => {
		const visible = filterMonitors(monitors, 'qa', []);
		const strip = buildOutside(monitors, visible);

		expect(strip).toEqual({
			text: 'Fuera de este filtro: 1 sin respuesta · 1 degradado',
			items: [
				{ id: 'pagos-api-prod', label: 'Pagos · API', env: ' · Prod', status: 'down', service: 'Pagos', environment: 'prod' },
				{ id: 'bo-api-dev', label: 'Backoffice · API', env: ' · Dev', status: 'degraded', service: 'Backoffice', environment: 'dev' }
			]
		});
	});

	it('ignores hidden monitors that are up or still pending', () => {
		const visible = filterMonitors(monitors, 'prod', ['Pagos']);
		expect(buildOutside(monitors, visible)?.items.map((i) => i.id)).toEqual(['bo-api-dev']);
	});

	it('pluralizes and labels a monitor without a service by its name', () => {
		const all = [
			makeMonitor('Webhook', null, 'api', null, 'degraded'),
			makeMonitor('bo', 'Backoffice', 'api', 'prod', 'degraded'),
			makeMonitor('pagos', 'Pagos', 'api', 'prod')
		];
		const strip = buildOutside(all, filterMonitors(all, 'all', ['Pagos']));

		expect(strip?.text).toBe('Fuera de este filtro: 2 degradados');
		expect(strip?.items.map((i) => [i.label, i.env])).toEqual([
			['Backoffice · API', ' · Prod'],
			['Webhook', '']
		]);
	});
});

describe('emptyStateCopy', () => {
	it('invites to add the first monitor when there are none', () => {
		expect(emptyStateCopy(0, 'prod', ['Pagos'])).toEqual({
			title: 'Todavía no hay monitores',
			sub: 'Agrega la URL de un front o el endpoint de health de una API para empezar.',
			canClear: false
		});
	});

	it('keeps the environment-only copy when no service is selected', () => {
		expect(emptyStateCopy(4, 'dev', [])).toEqual({
			title: 'No hay monitores en Dev',
			sub: 'Elige otro ambiente o agrega un monitor en Dev.',
			canClear: false
		});
	});

	it('names the service selection and offers to clear the filters', () => {
		expect(emptyStateCopy(4, 'qa', ['Backoffice'])).toMatchObject({ title: 'No hay monitores de Backoffice en QA', canClear: true });
		expect(emptyStateCopy(4, 'prod', [''])).toMatchObject({ title: 'No hay monitores sin servicio en Prod' });
		expect(emptyStateCopy(4, 'all', ['Pagos', 'Backoffice']).title).toBe('No hay monitores de los servicios elegidos');
	});
});
