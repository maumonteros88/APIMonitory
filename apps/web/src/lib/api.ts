import { clearToken, getToken } from './auth';

export interface LastCheck {
	at: string;
	httpStatus: number;
	latencyMs: number;
	errorKind: string;
}

export interface DayHistory {
	date: string;
	state: 'none' | 'op' | 'deg' | 'part' | 'major';
	downMinutes: number;
	degradedMinutes: number;
	checks: number;
	fails: number;
}

export interface Incident {
	state: 'down' | 'degraded';
	start: string;
	end: string;
	ongoing: boolean;
}

export interface IncidentsResponse {
	day: string;
	incidents: Incident[];
	partial: boolean;
}

export interface Monitor {
	id: string;
	name: string;
	description: string;
	url: string;
	status: 'pending' | 'up' | 'degraded' | 'down';
	checking: boolean;
	lastCheck: LastCheck | null;
	commit: string | null;
	uptimePercent: number | null;
	history: DayHistory[];
}

export interface MonitorsResponse {
	intervalSeconds: number;
	days: number;
	history: 'full' | 'recent';
	monitors: Monitor[];
}

export interface NewMonitorInput {
	name: string;
	url: string;
	description: string;
}

export interface MonitorDetailsInput {
	name: string;
	description: string;
}

export class ApiError extends Error {
	status: number;

	constructor(status: number, message: string) {
		super(message);
		this.status = status;
	}
}

export class UnauthorizedError extends ApiError {
	constructor() {
		super(401, 'Token de autenticación inválido o ausente');
	}
}

type UnauthorizedListener = () => void;
const unauthorizedListeners = new Set<UnauthorizedListener>();

/** Registers a callback fired whenever any request (other than an explicit
 * token probe) comes back 401, so the page can drop back to the token gate. */
export function onUnauthorized(listener: UnauthorizedListener): () => void {
	unauthorizedListeners.add(listener);
	return () => unauthorizedListeners.delete(listener);
}

async function safeJson(res: Response): Promise<{ error?: string } | null> {
	try {
		return await res.json();
	} catch {
		return null;
	}
}

async function request<T>(path: string, options: RequestInit = {}, tokenOverride?: string): Promise<T> {
	const token = tokenOverride ?? getToken();
	const headers = new Headers(options.headers);
	if (token) headers.set('Authorization', `Bearer ${token}`);
	if (options.body) headers.set('Content-Type', 'application/json');

	const res = await fetch(path, { ...options, headers });

	if (res.status === 401) {
		if (tokenOverride === undefined) {
			clearToken();
			for (const listener of unauthorizedListeners) listener();
		}
		throw new UnauthorizedError();
	}

	if (!res.ok) {
		const body = await safeJson(res);
		throw new ApiError(res.status, body?.error ?? 'Ocurrió un error inesperado');
	}

	if (res.status === 204) return undefined as T;
	return (await res.json()) as T;
}

/** True if a is a strictly older lastCheck than b -- used to keep the
 * poll (every 60s) and the SSE event stream from stepping on each other if
 * a slow poll response resolves after a newer event already landed. */
function isOlder(a: LastCheck | null, b: LastCheck | null): boolean {
	return a !== null && b !== null && a.at < b.at;
}

/** Inserts m into list, or replaces the existing entry with the same id --
 * unless the existing entry's lastCheck is already newer than m's, in which
 * case list is returned unchanged. */
export function upsertMonitor(list: Monitor[], m: Monitor): Monitor[] {
	const index = list.findIndex((existing) => existing.id === m.id);
	if (index === -1) return [...list, m];
	if (isOlder(m.lastCheck, list[index].lastCheck)) return list;
	return list.map((item, i) => (i === index ? m : item));
}

/** Replaces current with fetched (a fresh GET /api/monitors result), except
 * that any monitor whose current entry is newer than the fetched one (the
 * same poll/event race upsertMonitor guards against) keeps its current
 * entry instead. A monitor missing from fetched (deleted server-side) is
 * dropped, same as a plain replace would do. */
export function mergeMonitors(current: Monitor[], fetched: Monitor[]): Monitor[] {
	const byId = new Map(current.map((m) => [m.id, m]));
	return fetched.map((m) => {
		const existing = byId.get(m.id);
		return existing && isOlder(m.lastCheck, existing.lastCheck) ? existing : m;
	});
}

export interface MergeResult {
	monitors: Monitor[];
	needsResync: boolean;
}

/** Returns the calendar day after date (a "YYYY-MM-DD" string), via UTC
 * string arithmetic -- the client never derives "today" from the browser's
 * own clock/timezone, only from dates the server already sent. */
function nextDay(date: string): string {
	const d = new Date(`${date}T00:00:00Z`);
	d.setUTCDate(d.getUTCDate() + 1);
	return d.toISOString().slice(0, 10);
}

/** Applies one delta update -- an SSE "monitor" frame or a `history: 'recent'`
 * fetchMonitors entry, whose history is only the last couple of days,
 * not the monitor's full window -- onto list. Unlike upsertMonitor, this
 * can't just replace the existing entry outright: update.history has to be
 * merged day-by-day into the existing window instead.
 *
 * needsResync is true whenever that merge can't be done safely: an unknown
 * monitor id, or a delta whose first date isn't already present in the
 * existing entry's history, or whose next dates are each neither already
 * present nor exactly the day after the window's current last date. Any of
 * those means the local window and the delta don't provably line up (a
 * missed event, a sleep/reconnect gap, a day skipped entirely), and the
 * caller is expected to fall back to a full resync (loadMonitors) instead of
 * risking a gap or a duplicate date in the history array. */
export function applyMonitorDelta(list: Monitor[], update: Monitor): MergeResult {
	const index = list.findIndex((existing) => existing.id === update.id);
	if (index === -1) return { monitors: list, needsResync: true };

	const existing = list[index];
	if (isOlder(update.lastCheck, existing.lastCheck)) {
		return { monitors: list, needsResync: false };
	}
	if (!existing.history.some((d) => d.date === update.history[0].date)) {
		return { monitors: list, needsResync: true };
	}

	let history = existing.history;
	for (const day of update.history) {
		const dayIndex = history.findIndex((d) => d.date === day.date);
		if (dayIndex !== -1) {
			history = history.map((d, i) => (i === dayIndex ? day : d));
			continue;
		}
		if (day.date !== nextDay(history[history.length - 1].date)) {
			return { monitors: list, needsResync: true };
		}
		// Window length never changes: a genuinely new day slides it forward
		// by dropping the oldest entry instead of growing the array.
		history = [...history.slice(1), day];
	}

	const merged: Monitor = { ...update, history };
	return { monitors: list.map((m, i) => (i === index ? merged : m)), needsResync: false };
}

/** The `history: 'recent'` counterpart to mergeMonitors: applies a
 * fetchMonitors(days, 'recent') response by running applyMonitorDelta per
 * monitor still present in fetched, and dropping any monitor missing from it
 * (deleted server-side, same as mergeMonitors). If any single delta
 * needsResync -- including a fetched monitor unknown locally, e.g. one
 * created between polls -- the whole thing is discarded and current is
 * returned untouched: partially applying some monitors while leaving others
 * to a full resync would leave the list in a mixed state the caller's own
 * loadMonitors() should replace wholesale instead. */
export function mergeRecentMonitors(current: Monitor[], fetched: Monitor[]): MergeResult {
	const fetchedIds = new Set(fetched.map((m) => m.id));
	let monitors = current.filter((m) => fetchedIds.has(m.id));

	for (const update of fetched) {
		const result = applyMonitorDelta(monitors, update);
		if (result.needsResync) return { monitors: current, needsResync: true };
		monitors = result.monitors;
	}

	return { monitors, needsResync: false };
}

export function fetchMonitors(days = 90, history: 'full' | 'recent' = 'full'): Promise<MonitorsResponse> {
	const historyParam = history === 'recent' ? '&history=recent' : '';
	return request<MonitorsResponse>(`/api/monitors?days=${days}${historyParam}`);
}

/** Probes a candidate token before it's persisted: on 401 it throws without
 * touching the stored token or notifying onUnauthorized listeners. */
export function probeToken(token: string): Promise<MonitorsResponse> {
	return request<MonitorsResponse>('/api/monitors?days=90', {}, token);
}

export function createMonitor(input: NewMonitorInput): Promise<Monitor> {
	return request<Monitor>('/api/monitors', {
		method: 'POST',
		body: JSON.stringify(input)
	});
}

export function updateMonitor(id: string, input: MonitorDetailsInput): Promise<Monitor> {
	return request<Monitor>(`/api/monitors/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(input)
	});
}

export function deleteMonitor(id: string): Promise<void> {
	return request<void>(`/api/monitors/${id}`, { method: 'DELETE' });
}

export function checkMonitor(id: string): Promise<Monitor> {
	return request<Monitor>(`/api/monitors/${id}/check`, { method: 'POST' });
}

export function checkAllMonitors(): Promise<void> {
	return request<void>('/api/monitors/check', { method: 'POST' });
}

export function fetchIncidents(id: string, day: string): Promise<IncidentsResponse> {
	return request<IncidentsResponse>(`/api/monitors/${id}/incidents?day=${day}`);
}

const incidentsCache = new Map<string, Promise<IncidentsResponse>>();

/** Caches fetchIncidents by `id|day|checks` -- checks (the day's check count,
 * from the matching DayHistory entry) changes every time a new scheduled
 * check lands for a day still in progress (via SSE or the recent-history
 * poll), so a past day's entry is cached forever while today's key naturally
 * busts itself as new checks arrive. A rejected promise is evicted instead
 * of cached, so a transient failure doesn't poison the cache forever. */
export function fetchIncidentsCached(id: string, day: string, checks: number): Promise<IncidentsResponse> {
	const key = `${id}|${day}|${checks}`;
	const cached = incidentsCache.get(key);
	if (cached) return cached;

	const promise = fetchIncidents(id, day);
	incidentsCache.set(key, promise);
	promise.catch(() => incidentsCache.delete(key));
	return promise;
}

const EVENTS_RETRY_MS = 3000;

/** Subscribes to GET /api/events: onMonitor fires once per "monitor" frame
 * (a Monitor whose history is trimmed to just its last couple of days --
 * every other field is the monitor's full current state; merge it with
 * applyMonitorDelta, not upsertMonitor), and onConnectionChange(true) fires
 * on any frame at all (including the periodic heartbeat comment) as a
 * liveness signal, onConnectionChange(false) when the connection drops.
 *
 * Deliberately fetch + a hand-rolled parser instead of the browser's native
 * EventSource: EventSource can't set the Authorization header, and it can't
 * see a 401 either (it just gets a generic error with no status), which
 * would make a rotated token unrecoverable without a full page reload. This
 * reuses the exact same header-based auth (and the same onUnauthorized
 * listeners) as every other request.
 *
 * Returns a function that closes the connection and stops reconnecting. */
export function subscribeToMonitorEvents(
	onMonitor: (m: Monitor) => void,
	onConnectionChange: (ok: boolean) => void
): () => void {
	let closed = false;
	let controller: AbortController | null = null;
	let retryTimer: ReturnType<typeof setTimeout> | undefined;

	function handleFrame(frame: string) {
		let eventType = '';
		let data = '';
		for (const rawLine of frame.split('\n')) {
			const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine;
			if (line.startsWith('event: ')) eventType = line.slice('event: '.length);
			else if (line.startsWith('data: ')) data = line.slice('data: '.length);
			else if (line.startsWith(':')) eventType ||= 'heartbeat';
		}
		if (!eventType) return;
		onConnectionChange(true);
		if (eventType === 'monitor' && data) {
			try {
				onMonitor(JSON.parse(data) as Monitor);
			} catch {
				// Malformed frame: skip it, the next reconciliation poll settles it.
			}
		}
	}

	async function connect() {
		if (closed) return;
		controller = new AbortController();

		try {
			const token = getToken();
			const res = await fetch('/api/events', {
				headers: token ? { Authorization: `Bearer ${token}` } : {},
				signal: controller.signal
			});

			if (res.status === 401) {
				closed = true; // a rotated/invalid token won't fix itself on retry
				clearToken();
				for (const listener of unauthorizedListeners) listener();
				return;
			}
			if (!res.ok || !res.body) {
				throw new ApiError(res.status, 'No se pudo conectar al canal de eventos');
			}

			const reader = res.body.getReader();
			const decoder = new TextDecoder();
			let buffer = '';
			for (;;) {
				const { value, done } = await reader.read();
				if (done) break;
				buffer += decoder.decode(value, { stream: true });

				let sep: number;
				while ((sep = buffer.indexOf('\n\n')) !== -1) {
					handleFrame(buffer.slice(0, sep));
					buffer = buffer.slice(sep + 2);
				}
			}
		} catch {
			// Aborted by our own close(), or a genuine network/stream error --
			// either way there's nothing more to do here than let the retry
			// (below) or the caller's close() decide what happens next.
		} finally {
			if (!closed) {
				onConnectionChange(false);
				retryTimer = setTimeout(connect, EVENTS_RETRY_MS);
			}
		}
	}

	connect();

	return () => {
		closed = true;
		if (retryTimer) clearTimeout(retryTimer);
		controller?.abort();
	};
}
