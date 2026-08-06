import type {
  Health,
  DetailedHealth,
  LoginRequest,
  LoginResponse,
  User,
  SessionStatus,
  PAT,
  CreatePATRequest,
  CreatePATResponse,
  APIKey,
  Provider,
  CreateProviderRequest,
  Error as APIError,
} from '@/generated/admin-v1';

const BASE = '/api/admin/v1';

// CSRF double-submit patrol (design §12; a11y A2). The backend issues a
// JS-readable cookie named `gorouter_csrf` on safe (non-mutating) requests and
// requires an exact echo via the `X-CSRF-Token` header on every mutation. The
// client reads the cookie after the first safe GET and echoes it on mutations.
const CSRF_COOKIE = 'gorouter_csrf';
const CSRF_HEADER = 'X-CSRF-Token';
const MUTATING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

function readCsrfCookie(): string | null {
  if (typeof document === 'undefined') return null;
  const prefix = `${CSRF_COOKIE}=`;
  const cookies = document.cookie.split(';').map((c) => c.trim());
  const found = cookies.find((c) => c.startsWith(prefix));
  if (!found) return null;
  const value = found.slice(prefix.length);
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

let csrfToken: string | null = null;
let csrfLatched = false;

/** Internal reset for tests only. */
export function _resetCsrfStateForTests(): void {
  csrfToken = null;
  csrfLatched = false;
}

function resolveCsrfToken(): string | null {
  if (!csrfLatched) {
    csrfToken = readCsrfCookie();
    csrfLatched = true;
  }
  return csrfToken;
}

async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const method = (options.method ?? 'GET').toUpperCase();
  const mutating = MUTATING_METHODS.has(method);

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> | undefined),
  };

  if (mutating) {
    const token = resolveCsrfToken();
    if (token) headers[CSRF_HEADER] = token;
  }

  const res = await fetch(`${BASE}${path}`, {
    ...options,
    method,
    headers,
    credentials: 'include',
  });

  // After any safe response the server may have (re)issued the CSRF cookie;
  // re-latch so subsequent mutations echo the freshest token. Re-latching on
  // every safe method keeps the double-submit panel in sync across rotations.
  if (!mutating) {
    csrfToken = readCsrfCookie();
    csrfLatched = true;
  }

  if (!res.ok) {
    const err: APIError = await res.json().catch(() => ({
      code: 'UNKNOWN',
      message: `HTTP ${res.status}`,
    }));
    throw err;
  }

  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

// Health
export function getHealth(): Promise<Health> {
  return request('/health');
}

export function getDetailedHealth(): Promise<DetailedHealth> {
  return request('/health/detailed');
}

// Auth
export function login(data: LoginRequest): Promise<LoginResponse> {
  return request('/auth/login', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export function logout(): Promise<void> {
  return request('/auth/logout', { method: 'POST' });
}

export function getCurrentUser(): Promise<User> {
  return request('/auth/me');
}

export function getSessionStatus(): Promise<SessionStatus> {
  return request('/auth/status');
}

// PATs
export function listPATs(): Promise<PAT[]> {
  return request('/pats');
}

export function createPAT(data: CreatePATRequest): Promise<CreatePATResponse> {
  return request('/pats', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export function revokePAT(id: string): Promise<void> {
  return request(`/pats/${id}`, { method: 'DELETE' });
}

// API Keys
export function listAPIKeys(): Promise<APIKey[]> {
  return request('/api-keys');
}

export function revokeAPIKey(id: string): Promise<void> {
  return request(`/api-keys/${id}`, { method: 'DELETE' });
}

// Providers
export function listProviders(): Promise<Provider[]> {
  return request('/providers');
}

export function createProvider(data: CreateProviderRequest): Promise<Provider> {
  return request('/providers', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export function updateProvider(
  id: string,
  data: CreateProviderRequest,
): Promise<Provider> {
  return request(`/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export function deleteProvider(id: string): Promise<void> {
  return request(`/providers/${id}`, { method: 'DELETE' });
}
