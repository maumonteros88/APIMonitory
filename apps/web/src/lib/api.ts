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

export interface Monitor {
	id: string;
	name: string;
	description: string;
	url: string;
	status: 'pending' | 'up' | 'degraded' | 'down';
	checking: boolean;
	lastCheck: LastCheck | null;
	uptimePercent: number | null;
	history: DayHistory[];
}

export interface MonitorsResponse {
	intervalSeconds: number;
	days: number;
	monitors: Monitor[];
}

export interface NewMonitorInput {
	name: string;
	url: string;
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

export function fetchMonitors(days = 90): Promise<MonitorsResponse> {
	return request<MonitorsResponse>(`/api/monitors?days=${days}`);
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

export function deleteMonitor(id: string): Promise<void> {
	return request<void>(`/api/monitors/${id}`, { method: 'DELETE' });
}

export function checkMonitor(id: string): Promise<Monitor> {
	return request<Monitor>(`/api/monitors/${id}/check`, { method: 'POST' });
}

export function checkAllMonitors(): Promise<void> {
	return request<void>('/api/monitors/check', { method: 'POST' });
}
