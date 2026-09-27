export type DayState = 'none' | 'op' | 'deg' | 'part' | 'major';
export type MonitorStatus = 'pending' | 'up' | 'degraded' | 'down';

export interface StatusMeta {
	label: string;
	dotColor: string;
	textColor: string;
}

const MONTHS_ES = ['ene', 'feb', 'mar', 'abr', 'may', 'jun', 'jul', 'ago', 'sep', 'oct', 'nov', 'dic'];

/** Parses a "YYYY-MM-DD" calendar date as local time, avoiding the UTC-shift
 * that `new Date(dateStr)` would introduce for date-only strings. */
function parseDateOnly(dateStr: string): Date {
	const [y, m, d] = dateStr.split('-').map(Number);
	return new Date(y, m - 1, d);
}

export function fmtDate(dateStr: string): string {
	const date = parseDateOnly(dateStr);
	return `${date.getDate()} ${MONTHS_ES[date.getMonth()]} ${date.getFullYear()}`;
}

export function fmtMins(totalMinutes: number): string {
	const mins = Math.round(totalMinutes);
	const hours = Math.floor(mins / 60);
	const remMin = mins % 60;
	if (hours === 0) return `${remMin} min`;
	if (remMin === 0) return `${hours} h`;
	return `${hours} h ${remMin} min`;
}

/** Extracts "HH:MM" from an incident start/end timestamp. No `Intl` or
 * `Date` involved on purpose: the backend already formats these in
 * APP_TIMEZONE (see GET /api/monitors/{id}/incidents), so re-parsing them
 * with the browser's own clock/timezone would be redundant at best and, for
 * `APP_TIMEZONE=Local`, can throw a `RangeError` at worst. */
export function fmtTime(iso: string): string {
	return iso.slice(11, 16);
}

export function ago(iso: string, now: Date = new Date()): string {
	const then = new Date(iso);
	const diffSec = Math.max(0, Math.floor((now.getTime() - then.getTime()) / 1000));
	if (diffSec < 60) return 'hace instantes';
	const diffMin = Math.floor(diffSec / 60);
	if (diffMin < 60) return `hace ${diffMin} min`;
	const diffHours = Math.floor(diffMin / 60);
	if (diffHours < 24) return `hace ${diffHours} h`;
	const diffDays = Math.floor(diffHours / 24);
	return `hace ${diffDays} d`;
}

export function uptimeLabel(percent: number | null): string {
	if (percent === null) return 'Sin datos';
	return `${percent.toFixed(2)}%`;
}

const ERROR_LABELS: Record<string, string> = {
	timeout: 'Tiempo de espera agotado',
	dns: 'Error de resolución DNS',
	connection_refused: 'Conexión rechazada',
	tls: 'Error de certificado TLS',
	http_status: 'Respuesta HTTP inesperada',
	blocked: 'Destino bloqueado',
	other: 'Error desconocido'
};

export function errorLabel(errorKind: string): string {
	return ERROR_LABELS[errorKind] ?? 'Error desconocido';
}

const DAY_COLORS: Record<DayState, string> = {
	none: 'var(--m-none)',
	op: '#289B4F',
	deg: '#FFC700',
	part: '#FF6C3B',
	major: '#DF4730'
};

export function dayColor(state: DayState): string {
	return DAY_COLORS[state];
}

const STATUS_META: Record<MonitorStatus, StatusMeta> = {
	pending: { label: 'Verificando', dotColor: 'var(--m-none)', textColor: 'var(--fg-2)' },
	up: { label: 'Operativa', dotColor: '#289B4F', textColor: 'var(--m-ok-tx)' },
	degraded: { label: 'Degradada', dotColor: '#FFC700', textColor: 'var(--m-ink)' },
	down: { label: 'Sin respuesta', dotColor: '#DF4730', textColor: 'var(--m-bad-tx)' }
};

export function statusMeta(status: MonitorStatus): StatusMeta {
	return STATUS_META[status];
}

export interface TileStyle {
	bg: string;
	fg: string;
}

/** Board-view tile colors. Unlike statusMeta's dot/text colors, "up" shades
 * darker green as uptime drops, so a struggling-but-technically-up monitor
 * still stands out from a solidly healthy one. */
export function tileStyle(status: MonitorStatus, uptimePercent: number | null): TileStyle {
	if (status === 'up') {
		const bg =
			uptimePercent === null || uptimePercent >= 99.5
				? '#289B4F'
				: uptimePercent >= 98
					? '#23854A'
					: '#1E6E3D';
		return { bg, fg: '#FFFFFF' };
	}
	if (status === 'degraded') return { bg: '#FFC700', fg: '#191919' };
	if (status === 'down') return { bg: '#DF4730', fg: '#FFFFFF' };
	return { bg: 'var(--m-none)', fg: 'var(--m-ink)' };
}

/** Compact uptime percent for board tiles (no " disponibilidad" suffix). */
export function uptimeShort(percent: number | null): string {
	if (percent === null) return 'Sin datos';
	return `${percent.toFixed(2).replace('.', ',')} %`;
}
