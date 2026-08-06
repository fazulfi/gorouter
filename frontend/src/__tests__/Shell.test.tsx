import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import Shell from '@/app/components/Shell';
import Page from '@/app/components/Page';

vi.mock('@/shared/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/shared/api/client')>();
  return {
    ...actual,
    getSessionStatus: vi.fn().mockResolvedValue({ authenticated: true }),
    getCurrentUser: vi.fn().mockResolvedValue({ id: 'u1', email: 'a@b.c' }),
  };
});

import AuthGuard from '@/app/components/AuthGuard';

function renderShell() {
  const router = createMemoryRouter(
    [
      {
        element: (
          <AuthGuard>
            <Shell />
          </AuthGuard>
        ),
        children: [
          { path: '/', element: <Page title="Dashboard" /> },
          { path: '/providers', element: <Page title="Providers" /> },
        ],
      },
    ],
    { initialEntries: ['/'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Shell layout (a11y landmarks)', () => {
  beforeEach(() => {
    document.title = '';
  });

  it('renders aside, nav, header and main landmarks', async () => {
    renderShell();
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toBeTruthy(),
    );
    expect(document.querySelector('aside')).toBeTruthy();
    expect(document.querySelector('nav')).toBeTruthy();
    expect(document.querySelector('header')).toBeTruthy();
    expect(document.querySelector('main')).toBeTruthy();
  });

  it('renders the skip link as the first focusable element', async () => {
    const { container } = renderShell();
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toBeTruthy(),
    );
    const skip = screen.getByRole('link', { name: 'Skip to main content' });
    const focusables = container.querySelectorAll<HTMLElement>(
      'a[href], button, [tabindex]:not([tabindex="-1"])',
    );
    expect(focusables[0]).toBe(skip);
  });

  it('renders a single h1 inside main', async () => {
    renderShell();
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toBeTruthy(),
    );
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  });

  it('sets the document title for a route', async () => {
    renderShell();
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toBeTruthy(),
    );
    expect(document.title).toBe('Dashboard - gorouter');
  });

  it('marks the active nav item with aria-current="page"', async () => {
    renderShell();
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toBeTruthy(),
    );
    const link = screen.getByRole('link', { name: 'Dashboard' });
    expect(link.getAttribute('aria-current')).toBe('page');
  });
});