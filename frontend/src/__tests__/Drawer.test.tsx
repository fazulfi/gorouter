import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { RouterProvider, createMemoryRouter } from 'react-router-dom';
import Drawer from '@/app/components/Drawer';
import { useAuthStore } from '@/app/stores/authStore';

vi.mock('@/shared/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/shared/api/client')>();
  return {
    ...actual,
    getSessionStatus: vi.fn().mockResolvedValue({ authenticated: true }),
  };
});

import { getSessionStatus } from '@/shared/api/client';

function renderDrawer() {
  const router = createMemoryRouter(
    [{ path: '/', element: <Drawer /> }],
    { initialEntries: ['/'] },
  );
  return render(<RouterProvider router={router} />);
}

describe('Mobile drawer (a11y A4, A10-A12)', () => {
  beforeEach(() => {
    useAuthStore.setState({ state: 'authenticated', user: null });
    vi.mocked(getSessionStatus).mockResolvedValue({
      authenticated: true,
      user: { id: 'u1', email: 'a@b.c' },
    });
  });

  it('exposes aria-expanded and aria-controls on the trigger', () => {
    renderDrawer();
    const trigger = screen.getByRole('button', { name: 'Open navigation menu' });
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(trigger.getAttribute('aria-controls')).toBe('primary-nav');
  });

  it('opens the drawer and sets aria-expanded true', () => {
    renderDrawer();
    const trigger = screen.getByRole('button', { name: 'Open navigation menu' });
    fireEvent.click(trigger);
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(document.querySelector('aside[id="primary-nav"]')).toBeTruthy();
  });

  it('closes on Escape and restores focus to the trigger', () => {
    renderDrawer();
    const trigger = screen.getByRole('button', { name: 'Open navigation menu' });
    fireEvent.click(trigger);
    const closeBtn = screen.getByRole('button', { name: 'Close navigation menu' });
    closeBtn.focus();
    fireEvent.keyDown(closeBtn, { key: 'Escape' });
    expect(document.querySelector('aside[id="primary-nav"]')).toBeNull();
    expect(trigger).toBe(document.activeElement);
  });

  it('closes on the close button', () => {
    renderDrawer();
    fireEvent.click(screen.getByRole('button', { name: 'Open navigation menu' }));
    fireEvent.click(screen.getByRole('button', { name: 'Close navigation menu' }));
    expect(document.querySelector('aside[id="primary-nav"]')).toBeNull();
  });
});