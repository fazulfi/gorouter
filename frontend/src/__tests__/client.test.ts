import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  getHealth,
  getDetailedHealth,
  login,
  logout,
  getCurrentUser,
  listPATs,
  createPAT,
  revokePAT,
  listAPIKeys,
  revokeAPIKey,
  listProviders,
  createProvider,
  updateProvider,
  deleteProvider,
  _resetCsrfStateForTests,
} from '@/shared/api/client';

const mockFetch = vi.fn();

beforeEach(() => {
  mockFetch.mockReset();
  globalThis.fetch = mockFetch;
  _resetCsrfStateForTests();
});

afterEach(() => {
  _resetCsrfStateForTests();
});

describe('API client', () => {
  describe('getHealth', () => {
    it('returns health data on success', async () => {
      const data = { status: 'ok' as const, timestamp: '2024-06-01T00:00:00Z' };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(data),
      });

      const result = await getHealth();
      expect(result).toEqual(data);
    });

    it('sends GET to /api/admin/v1/health', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', timestamp: '' }),
      });

      await getHealth();
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/health',
        expect.objectContaining({
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
        }),
      );
    });
  });

  describe('getDetailedHealth', () => {
    it('returns detailed health data', async () => {
      const data = {
        status: 'ok' as const,
        timestamp: '2024-06-01T00:00:00Z',
        version: '1.0.0',
        uptime_seconds: 3600,
        db: { connected: true, pool_conns_in_use: 2, pool_conns_idle: 8 },
        goroutines: 12,
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(data),
      });

      expect(await getDetailedHealth()).toEqual(data);
    });
  });

  describe('login', () => {
    it('sends POST with JSON body and returns session', async () => {
      const credentials = { email: 'admin@test.com', password: 's3cret' };
      const response = {
        session_id: 'sess_001',
        user: {
          id: 'u1',
          email: 'admin@test.com',
          is_admin: true,
          created_at: '2024-01-01T00:00:00Z',
        },
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(response),
      });

      const result = await login(credentials);
      expect(result).toEqual(response);
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/auth/login',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify(credentials),
        }),
      );
    });
  });

  describe('logout', () => {
    it('handles 204 empty response gracefully', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve({}),
      });

      const result = await logout();
      expect(result).toBeUndefined();
    });
  });

  describe('getCurrentUser', () => {
    it('returns the current user', async () => {
      const user = {
        id: 'u1',
        email: 'admin@test.com',
        is_admin: true,
        created_at: '2024-01-01T00:00:00Z',
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(user),
      });

      expect(await getCurrentUser()).toEqual(user);
    });
  });

  describe('PATs', () => {
    it('listPATs returns an array of PATs', async () => {
      const pats = [
        {
          id: 'pat_1',
          token_prefix: 'grp_abc',
          created_at: '2024-01-01T00:00:00Z',
        },
      ];
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(pats),
      });

      expect(await listPATs()).toEqual(pats);
    });

    it('createPAT sends POST with body', async () => {
      const req = { description: 'dev token' };
      const res = { id: 'pat_1', token: 'grp_secret', description: 'dev token', created_at: '' };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(res),
      });

      expect(await createPAT(req)).toEqual(res);
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/pats',
        expect.objectContaining({ method: 'POST', body: JSON.stringify(req) }),
      );
    });

    it('revokePAT sends DELETE', async () => {
      mockFetch.mockResolvedValueOnce({ ok: true, status: 204, json: () => Promise.resolve({}) });

      await revokePAT('pat_1');
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/pats/pat_1',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });
  });

  describe('API Keys', () => {
    it('listAPIKeys returns an array', async () => {
      const keys = [{ id: 'ak_1', name: 'staging', key_prefix: 'grk_', created_at: '' }];
      mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(keys) });

      expect(await listAPIKeys()).toEqual(keys);
    });

    it('revokeAPIKey sends DELETE', async () => {
      mockFetch.mockResolvedValueOnce({ ok: true, status: 204, json: () => Promise.resolve({}) });

      await revokeAPIKey('ak_1');
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/api-keys/ak_1',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });
  });

  describe('Providers', () => {
    const providers = [
      {
        id: 'p1',
        name: 'OpenAI',
        type: 'openai' as const,
        base_url: 'https://api.openai.com/v1',
        is_enabled: true,
        created_at: '',
        updated_at: '',
      },
    ];

    it('listProviders returns an array', async () => {
      mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(providers) });
      expect(await listProviders()).toEqual(providers);
    });

    it('createProvider sends POST', async () => {
      const req = { name: 'Anthropic', type: 'anthropic' as const, base_url: 'https://api.anthropic.com' };
      mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ id: 'p2', ...req, is_enabled: true, created_at: '', updated_at: '' }) });

      const result = await createProvider(req);
      expect(result).toMatchObject({ name: 'Anthropic' });
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/providers',
        expect.objectContaining({ method: 'POST', body: JSON.stringify(req) }),
      );
    });

    it('updateProvider sends PUT', async () => {
      const req = { name: 'OpenAI', type: 'openai' as const, base_url: 'https://new.url/v1' };
      mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ id: 'p1', ...req, is_enabled: true, created_at: '', updated_at: '' }) });

      const result = await updateProvider('p1', req);
      expect(result).toMatchObject({ id: 'p1' });
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/providers/p1',
        expect.objectContaining({ method: 'PUT', body: JSON.stringify(req) }),
      );
    });

    it('deleteProvider sends DELETE', async () => {
      mockFetch.mockResolvedValueOnce({ ok: true, status: 204, json: () => Promise.resolve({}) });

      await deleteProvider('p1');
      expect(mockFetch).toHaveBeenCalledWith(
        '/api/admin/v1/providers/p1',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });
  });

  describe('error handling', () => {
    it('throws APIError on non-ok response with JSON body', async () => {
      const apiError = { code: 'UNAUTHORIZED', message: 'Invalid credentials' };
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () => Promise.resolve(apiError),
      });

      await expect(getCurrentUser()).rejects.toEqual(apiError);
    });

    it('falls back to generic error when JSON parsing fails', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: () => Promise.reject(new Error('parse error')),
      });

      await expect(getCurrentUser()).rejects.toEqual({
        code: 'UNKNOWN',
        message: 'HTTP 500',
      });
    });
  });

  describe('CSRF double-submit echo (a11y A2)', () => {
    it('echoes the gorouter_csrf cookie on mutations after a safe GET', async () => {
      Object.defineProperty(globalThis, 'document', {
        value: { cookie: 'gorouter_csrf=csrf-token-123; Path=/' },
        configurable: true,
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', timestamp: '' }),
      });
      await getHealth();

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve({}),
      });
      await deleteProvider('p1');

      const [, init] = mockFetch.mock.calls[1] as [
        string,
        RequestInit | undefined,
      ];
      expect(init?.headers).toEqual(
        expect.objectContaining({ 'X-CSRF-Token': 'csrf-token-123' }),
      );

      delete (globalThis as { document?: unknown }).document;
    });

    it('reads the CSRF cookie and echoes on a mutation without a prior GET', async () => {
      Object.defineProperty(globalThis, 'document', {
        value: { cookie: 'gorouter_csrf=token-no-get; Path=/' },
        configurable: true,
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve({}),
      });
      await createPAT({ description: 'dev' });

      const [, init] = mockFetch.mock.calls[0] as [
        string,
        RequestInit | undefined,
      ];
      expect(init?.headers).toEqual(
        expect.objectContaining({ 'X-CSRF-Token': 'token-no-get' }),
      );

      delete (globalThis as { document?: unknown }).document;
    });

    it('does not send X-CSRF-Token on safe GET requests', async () => {
      Object.defineProperty(globalThis, 'document', {
        value: { cookie: 'gorouter_csrf=csrf-token-123; Path=/' },
        configurable: true,
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', timestamp: '' }),
      });
      await getHealth();

      const [url, init] = mockFetch.mock.calls[0] as [
        string,
        RequestInit | undefined,
      ];
      expect(url).toBe('/api/admin/v1/health');
      expect(init?.headers).toEqual({ 'Content-Type': 'application/json' });

      delete (globalThis as { document?: unknown }).document;
    });

    it('sends no X-CSRF-Token when the cookie is absent', async () => {
      Object.defineProperty(globalThis, 'document', {
        value: { cookie: '' },
        configurable: true,
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve({}),
      });
      await logout();

      const [, init] = mockFetch.mock.calls[0] as [
        string,
        RequestInit | undefined,
      ];
      expect(init?.headers).not.toHaveProperty('X-CSRF-Token');

      delete (globalThis as { document?: unknown }).document;
    });

    it('re-latches the CSRF cookie after each safe response', async () => {
      Object.defineProperty(globalThis, 'document', {
        value: { cookie: 'gorouter_csrf=old-token; Path=/' },
        configurable: true,
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', timestamp: '' }),
      });
      await getHealth();

      Object.defineProperty(globalThis, 'document', {
        value: { cookie: 'gorouter_csrf=new-token; Path=/' },
        configurable: true,
      });

      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', timestamp: '' }),
      });
      await getHealth();

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve({}),
      });
      await logout();

      const [, init] = mockFetch.mock.calls[2] as [
        string,
        RequestInit | undefined,
      ];
      expect(init?.headers).toEqual(
        expect.objectContaining({ 'X-CSRF-Token': 'new-token' }),
      );

      delete (globalThis as { document?: unknown }).document;
    });
  });
});
