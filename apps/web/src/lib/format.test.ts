import { describe, expect, it } from 'vitest';
import { ago, dayColor, errorLabel, fmtDate, fmtMins, statusMeta, uptimeLabel } from './format';

describe('fmtDate', () => {
	it('formats a calendar date without shifting timezone', () => {
		expect(fmtDate('2026-09-26')).toBe('26 sep 2026');
	});

	it('formats the first day of a month', () => {
		expect(fmtDate('2026-06-01')).toBe('1 jun 2026');
	});

	it('formats the last day of the year', () => {
		expect(fmtDate('2026-12-31')).toBe('31 dic 2026');
	});
});

describe('fmtMins', () => {
	it('formats zero minutes', () => {
		expect(fmtMins(0)).toBe('0 min');
	});

	it('formats minutes under an hour', () => {
		expect(fmtMins(45)).toBe('45 min');
	});

	it('formats an exact hour with no remainder', () => {
		expect(fmtMins(60)).toBe('1 h');
	});

	it('formats hours with a minute remainder', () => {
		expect(fmtMins(1215)).toBe('20 h 15 min');
	});
});

describe('ago', () => {
	const now = new Date('2026-09-26T12:00:00Z');

	it('reports instants for sub-minute gaps', () => {
		expect(ago('2026-09-26T11:59:45Z', now)).toBe('hace instantes');
	});

	it('reports minutes', () => {
		expect(ago('2026-09-26T11:58:00Z', now)).toBe('hace 2 min');
	});

	it('reports hours', () => {
		expect(ago('2026-09-26T09:00:00Z', now)).toBe('hace 3 h');
	});

	it('reports days', () => {
		expect(ago('2026-09-23T12:00:00Z', now)).toBe('hace 3 d');
	});
});

describe('uptimeLabel', () => {
	it('formats a percentage with two decimals', () => {
		expect(uptimeLabel(99.8712)).toBe('99.87%');
	});

	it('formats a perfect uptime', () => {
		expect(uptimeLabel(100)).toBe('100.00%');
	});

	it('falls back to "Sin datos" when there is no data', () => {
		expect(uptimeLabel(null)).toBe('Sin datos');
	});
});

describe('errorLabel', () => {
	it.each([
		['timeout', 'Tiempo de espera agotado'],
		['dns', 'Error de resolución DNS'],
		['connection_refused', 'Conexión rechazada'],
		['tls', 'Error de certificado TLS'],
		['http_status', 'Respuesta HTTP inesperada'],
		['blocked', 'Destino bloqueado'],
		['other', 'Error desconocido']
	])('maps %s to its Spanish label', (kind, label) => {
		expect(errorLabel(kind)).toBe(label);
	});

	it('falls back to a generic label for an unknown kind', () => {
		expect(errorLabel('unknown_kind')).toBe('Error desconocido');
	});
});

describe('dayColor', () => {
	it.each([
		['none', 'var(--m-none)'],
		['op', '#289B4F'],
		['deg', '#FFC700'],
		['part', '#FF6C3B'],
		['major', '#DF4730']
	] as const)('maps %s to its color', (state, color) => {
		expect(dayColor(state)).toBe(color);
	});
});

describe('statusMeta', () => {
	it('maps pending', () => {
		expect(statusMeta('pending').label).toBe('Verificando');
	});

	it('maps up', () => {
		expect(statusMeta('up').label).toBe('Operativa');
	});

	it('maps degraded', () => {
		expect(statusMeta('degraded').label).toBe('Degradada');
	});

	it('maps down', () => {
		expect(statusMeta('down').label).toBe('Sin respuesta');
	});
});
