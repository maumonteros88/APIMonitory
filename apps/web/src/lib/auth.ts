const STORAGE_KEY = 'apimonitory_token';

export function getToken(): string | null {
	try {
		return localStorage.getItem(STORAGE_KEY);
	} catch {
		return null;
	}
}

export function setToken(token: string): void {
	try {
		localStorage.setItem(STORAGE_KEY, token);
	} catch {
		// Storage unavailable (private mode, disabled cookies): the gate will
		// just ask again next load instead of crashing.
	}
}

export function clearToken(): void {
	try {
		localStorage.removeItem(STORAGE_KEY);
	} catch {
		// See getToken.
	}
}
