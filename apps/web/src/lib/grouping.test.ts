import { describe, expect, it } from 'vitest';
import type { Environment, Monitor, MonitorKind } from './api';
import {
	buildBanners,
	buildBoard,
	buildMatrix,
	buildSegments,
	countLabel,
	envCounts,
	filterByEnv,
	groupByService,
	groupMeta,
	intervalLabel,
	isEnvFilter,
	kindsLabel,
	serviceNames,
	summaryLine
} from './grouping';

function makeMonitor(
	id: string,
	service: string | null,
	kind: MonitorKind,
	environment: Environment | null,
	overrides: Partial<Monitor> = {}
): Monitor {
	return {
		id,
		name: id,
		description: '',
		url: 'https://example.com',
		service,
		kind,
		environment,
		status: 'up',
		checking: false,
		lastCheck: null,
		commit: null,
		uptimePercent: 100,
		history: [],
		...overrides
	};
}

const ids = (monitors: Monitor[]) => monitors.map((m) => m.id);

describe('isEnvFilter', () => {
	it('accepts the four filters and nothing else', () => {
		for (const value of ['all', 'dev', 'qa', 'prod']) expect(isEnvFilter(value)).toBe(true);
		for (const value of ['Prod', 'staging', '', null, 1]) expect(isEnvFilter(value)).toBe(false);
	});
});

describe('countLabel', () => {
	it('pluralizes monitor', () => {
		expect(countLabel(0)).toBe('0 monitores');
		expect(countLabel(1)).toBe('1 monitor');
		expect(countLabel(2)).toBe('2 monitores');
	});
});

describe('filterByEnv and envCounts', () => {
	const monitors = [
		makeMonitor('a', 'Pagos', 'api', 'prod'),
		makeMonitor('b', 'Pagos', 'api', 'qa'),
		makeMonitor('c', 'Backoffice', 'front', 'prod'),
		makeMonitor('d', null, 'api', null)
	];

	it('keeps every monitor under all, including those without an environment', () => {
		expect(ids(filterByEnv(monitors, 'all'))).toEqual(['a', 'b', 'c', 'd']);
	});

	it('keeps only that environment otherwise', () => {
		expect(ids(filterByEnv(monitors, 'prod'))).toEqual(['a', 'c']);
		expect(ids(filterByEnv(monitors, 'dev'))).toEqual([]);
	});

	it('counts monitors per filter, with all counting everything', () => {
		expect(envCounts(monitors)).toEqual({ all: 4, dev: 0, qa: 1, prod: 2 });
	});
});

describe('groupByService', () => {
	it('sorts services alphabetically (es) and puts "no service" last', () => {
		const groups = groupByService([
			makeMonitor('x', null, 'api', 'prod'),
			makeMonitor('p', 'Pagos', 'api', 'prod'),
			makeMonitor('e', 'Ejemplo', 'api', 'prod'),
			makeMonitor('b', 'Backoffice', 'api', 'prod'),
			makeMonitor('a', 'Ámbito', 'api', 'prod')
		]);

		expect(groups.map((g) => g.service)).toEqual(['Ámbito', 'Backoffice', 'Ejemplo', 'Pagos', null]);
	});

	it('orders a service by environment (prod, qa, dev, none), then front before api, then name', () => {
		const [group] = groupByService([
			makeMonitor('dev-api', 'Pagos', 'api', 'dev'),
			makeMonitor('none-front', 'Pagos', 'front', null),
			makeMonitor('qa-api', 'Pagos', 'api', 'qa'),
			makeMonitor('prod-api-b', 'Pagos', 'api', 'prod'),
			makeMonitor('prod-front', 'Pagos', 'front', 'prod'),
			makeMonitor('prod-api-a', 'Pagos', 'api', 'prod'),
			makeMonitor('dev-front', 'Pagos', 'front', 'dev')
		]);

		expect(ids(group.monitors)).toEqual([
			'prod-front',
			'prod-api-a',
			'prod-api-b',
			'qa-api',
			'dev-front',
			'dev-api',
			'none-front'
		]);
	});

	it('returns no groups for no monitors', () => {
		expect(groupByService([])).toEqual([]);
	});
});

describe('kindsLabel and groupMeta', () => {
	it('describes which kinds a service has', () => {
		expect(kindsLabel([makeMonitor('a', 'Pagos', 'front', null), makeMonitor('b', 'Pagos', 'api', null)])).toBe(
			'Front y API'
		);
		expect(kindsLabel([makeMonitor('a', 'Pagos', 'front', null)])).toBe('Solo front');
		expect(kindsLabel([makeMonitor('a', 'Pagos', 'api', null)])).toBe('Solo API');
	});

	it('adds the kinds to a service group but only the count to "no service"', () => {
		const [pagos, none] = groupByService([
			makeMonitor('a', 'Pagos', 'front', 'prod'),
			makeMonitor('b', 'Pagos', 'api', 'prod'),
			makeMonitor('c', null, 'front', null)
		]);

		expect(groupMeta(pagos)).toBe('2 monitores · Front y API');
		expect(groupMeta(none)).toBe('1 monitor');
	});
});

describe('serviceNames', () => {
	it('lists each service once, sorted, without the empty one', () => {
		expect(
			serviceNames([
				makeMonitor('a', 'Pagos', 'api', 'prod'),
				makeMonitor('b', 'Backoffice', 'api', 'prod'),
				makeMonitor('c', 'Pagos', 'front', 'qa'),
				makeMonitor('d', null, 'api', null)
			])
		).toEqual(['Backoffice', 'Pagos']);
	});
});

describe('buildBanners', () => {
	it('says nothing is configured when no monitor is visible', () => {
		expect(buildBanners([], 'prod')).toEqual([{ level: 'none', text: 'Sin monitores configurados', items: [] }]);
	});

	it('says everything is fine when nothing is down or degraded (pending included)', () => {
		const banners = buildBanners(
			[makeMonitor('a', 'Pagos', 'api', 'prod'), makeMonitor('b', 'Pagos', 'front', 'prod', { status: 'pending' })],
			'prod'
		);

		expect(banners).toEqual([{ level: 'ok', text: 'Todo operativo', items: [] }]);
	});

	it('stacks down before degraded, each listing its monitors in list order', () => {
		const banners = buildBanners(
			[
				makeMonitor('deg', 'Pagos', 'api', 'prod', { status: 'degraded' }),
				makeMonitor('down-pagos', 'Pagos', 'front', 'prod', { status: 'down' }),
				makeMonitor('down-bo', 'Backoffice', 'api', 'prod', { status: 'down' }),
				makeMonitor('ok', 'Backoffice', 'front', 'prod')
			],
			'prod'
		);

		expect(banners).toEqual([
			{
				level: 'bad',
				text: '2 monitores sin respuesta',
				items: [
					{ id: 'down-bo', label: 'Backoffice · API', env: '' },
					{ id: 'down-pagos', label: 'Pagos · Front', env: '' }
				]
			},
			{
				level: 'warn',
				text: '1 monitor con rendimiento degradado',
				items: [{ id: 'deg', label: 'Pagos · API', env: '' }]
			}
		]);
	});

	it('labels a monitor without a service by its name, and adds the environment only under all', () => {
		const monitors = [
			makeMonitor('API de ejemplo', null, 'api', null, { status: 'down' }),
			makeMonitor('pagos', 'Pagos', 'api', 'qa', { status: 'down' })
		];

		expect(buildBanners(monitors, 'all')[0].items).toEqual([
			{ id: 'pagos', label: 'Pagos · API', env: ' · QA' },
			{ id: 'API de ejemplo', label: 'API de ejemplo', env: '' }
		]);
		expect(buildBanners(monitors.slice(1), 'qa')[0].items).toEqual([
			{ id: 'pagos', label: 'Pagos · API', env: '' }
		]);
	});
});

describe('intervalLabel and summaryLine', () => {
	it('formats the check interval in minutes when it is a whole number of them', () => {
		expect(intervalLabel(60)).toBe('1 min');
		expect(intervalLabel(300)).toBe('5 min');
		expect(intervalLabel(90)).toBe('90 s');
		expect(intervalLabel(30)).toBe('30 s');
	});

	it('names the selected environment, or all of them', () => {
		expect(summaryLine(4, 'dev', 60)).toBe('4 monitores en Dev · se verifica cada 1 min');
		expect(summaryLine(1, 'prod', 60)).toBe('1 monitor en Prod · se verifica cada 1 min');
		expect(summaryLine(13, 'all', 30)).toBe('13 monitores en todos los ambientes · se verifica cada 30 s');
	});

	it('is empty when no monitor is visible', () => {
		expect(summaryLine(0, 'prod', 60)).toBe('');
	});
});

describe('buildSegments', () => {
	it('puts fronts first, then APIs', () => {
		const segments = buildSegments([
			makeMonitor('api', 'Pagos', 'api', 'prod'),
			makeMonitor('front', 'Pagos', 'front', 'prod')
		]);

		expect(segments.map((s) => [s.kind, s.monitor?.id])).toEqual([
			['front', 'front'],
			['api', 'api']
		]);
	});

	it('adds a placeholder for the missing kind', () => {
		expect(buildSegments([makeMonitor('api', 'Pagos', 'api', 'prod')]).map((s) => [s.kind, s.monitor?.id ?? null])).toEqual([
			['front', null],
			['api', 'api']
		]);
		expect(buildSegments([makeMonitor('front', 'Pagos', 'front', 'prod')]).map((s) => [s.kind, s.monitor?.id ?? null])).toEqual([
			['front', 'front'],
			['api', null]
		]);
	});

	it('stacks several monitors of the same kind, by name', () => {
		const segments = buildSegments([
			makeMonitor('api-b', 'Pagos', 'api', 'prod'),
			makeMonitor('api-a', 'Pagos', 'api', 'prod'),
			makeMonitor('front', 'Pagos', 'front', 'prod')
		]);

		expect(segments.map((s) => s.monitor?.id)).toEqual(['front', 'api-a', 'api-b']);
	});

	it('is empty for no monitors', () => {
		expect(buildSegments([])).toEqual([]);
	});
});

describe('buildBoard', () => {
	it('makes one card per service and leaves the monitors without one aside', () => {
		const board = buildBoard([
			makeMonitor('pagos-api', 'Pagos', 'api', 'prod'),
			makeMonitor('loose-b', null, 'api', 'prod'),
			makeMonitor('bo-front', 'Backoffice', 'front', 'prod'),
			makeMonitor('bo-api', 'Backoffice', 'api', 'prod'),
			makeMonitor('loose-a', null, 'front', 'prod')
		]);

		expect(board.cards.map((c) => [c.service, c.meta, c.segments.map((s) => s.monitor?.id ?? null)])).toEqual([
			['Backoffice', 'Front y API', ['bo-front', 'bo-api']],
			['Pagos', 'Solo API', [null, 'pagos-api']]
		]);
		expect(ids(board.unassigned)).toEqual(['loose-a', 'loose-b']);
	});

	it('is empty for no monitors', () => {
		expect(buildBoard([])).toEqual({ cards: [], unassigned: [] });
	});
});

describe('buildMatrix', () => {
	const monitors = [
		makeMonitor('pagos-api-qa', 'Pagos', 'api', 'qa'),
		makeMonitor('pagos-api-prod', 'Pagos', 'api', 'prod'),
		makeMonitor('bo-front-dev', 'Backoffice', 'front', 'dev'),
		makeMonitor('bo-api-dev', 'Backoffice', 'api', 'dev'),
		makeMonitor('bo-front-prod', 'Backoffice', 'front', 'prod'),
		makeMonitor('bo-no-env', 'Backoffice', 'api', null),
		makeMonitor('no-service', null, 'api', 'prod'),
		makeMonitor('nothing', null, 'front', null)
	];

	it('makes one row per service, alphabetically, with dev / qa / prod cells', () => {
		const { rows } = buildMatrix(monitors);

		expect(rows.map((r) => r.service)).toEqual(['Backoffice', 'Pagos']);
		expect(rows.map((r) => r.cells.map((c) => c.env))).toEqual([
			['dev', 'qa', 'prod'],
			['dev', 'qa', 'prod']
		]);
	});

	it('fills each cell with its segments, or nothing for "Sin monitores"', () => {
		const [backoffice, pagos] = buildMatrix(monitors).rows;
		const cellIds = (row: typeof backoffice) => row.cells.map((c) => c.segments.map((s) => s.monitor?.id ?? null));

		expect(backoffice.meta).toBe('Front y API');
		expect(cellIds(backoffice)).toEqual([['bo-front-dev', 'bo-api-dev'], [], ['bo-front-prod', null]]);
		expect(pagos.meta).toBe('Solo API');
		expect(cellIds(pagos)).toEqual([[], [null, 'pagos-api-qa'], [null, 'pagos-api-prod']]);
	});

	it('sets aside monitors missing a service or an environment, in list order', () => {
		expect(ids(buildMatrix(monitors).unassigned)).toEqual(['bo-no-env', 'no-service', 'nothing']);
	});

	it('leaves out a service whose monitors all lack an environment', () => {
		const { rows, unassigned } = buildMatrix([makeMonitor('a', 'Ejemplo', 'api', null)]);

		expect(rows).toEqual([]);
		expect(ids(unassigned)).toEqual(['a']);
	});
});
