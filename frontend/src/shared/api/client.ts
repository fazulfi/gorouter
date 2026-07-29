import type {
  Health,
  DetailedHealth,
  LoginRequest,
  LoginResponse,
  User,
  PAT,
  CreatePATRequest,
  CreatePATResponse,
  APIKey,
  Provider,
  CreateProviderRequest,
  APIError,
} from '@/generated/admin-v1';

const BASE = '/api/admin/v1';

async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
    credentials: 'include',
    ...options,
  });

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
