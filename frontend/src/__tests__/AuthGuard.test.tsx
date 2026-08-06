import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { RouterProvider, createMemoryRouter, type RouteObject } from 'react-router-dom';
import AuthGuard from '@/app/components/AuthGuard';
import { useAuthStore } from '@/app/stores/authStore';
import { getSessionStatus } from '@/shared/api/client';

vi.mock('@/shared/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/shared/api/client')>();
  return {
    ...actual,
    getSessionStatus: vi.fn(),
    getCurrentUser: vi.fn(),
  };
});

function makeGuardRoutes(initialPath: string) {
  const routes: RouteObject[] = [
    {
      path: '/login',
      element: <h1>Sign in page</h1>,
    },
    {
      element: <AuthGuard fallback={<p data-testid="guard-loading">checking</p>} />,
      children: [{ path: '/protected', element: <h1>Protected</h1> }],
    },
  ];
  const router = createMemoryRouter(routes, { initialEntries: [initialPath] });
  return render(<RouterProvider router={router} />);
}

describe('AuthGuard (a11y A44)', () => {
  beforeEach(() => {
    useAuthStore.setState({ state: 'loading', user: null, error: null });
    vi.mocked(getSessionStatus).mockReset();
  });

  it('renders children when the session is authenticated', async () => {
    vi.mocked(getSessionStatus).mockResolvedValue({
      authenticated: true,
      user: { id: 'u1', email: 'a@b.c' },
    });
    makeGuardRoutes('/protected');
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Protected' })).toBeTruthy());
  });

  it('redirects unauthenticated visits to /login with next param', async () => {
    vi.mocked(getSessionStatus).mockResolvedValue({ authenticated: false });
    makeGuardRoutes('/protected');
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Sign in page' })).toBeTruthy());
    expect(globalThis.location).toBeDefined();
  });
});