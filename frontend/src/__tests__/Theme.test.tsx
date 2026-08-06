import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ThemeProvider from '@/app/providers/theme';
import ThemeToggle from '@/app/components/ThemeToggle';
import {
  useThemeStore,
  resolveInitialTheme,
  THEME_STORAGE_KEY,
  type Theme,
} from '@/app/stores/themeStore';

/**
 * Theme system (FE-03, D2/A8-A9). Precedence contract: stored manual override
 * > prefers-color-scheme > default light; the system-change listener must not
 * clobber a stored manual override; announces via role="status"
 * aria-live="polite" (A8-A9).
 */
describe('themeStore precedence + persistence (D2)', () => {
  let originalMatchMedia: typeof window.matchMedia;

  function noStorage() {
    return undefined;
  }

  beforeEach(() => {
    originalMatchMedia = window.matchMedia;
    localStorage.clear();
    useThemeStore.setState({ theme: 'light', resolvedTheme: 'light' });
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    localStorage.clear();
    useThemeStore.setState({ theme: 'light', resolvedTheme: 'light' });
  });

  function fakeSystem(scheme: 'light' | 'dark' | null) {
    const matches = scheme === 'dark';
    const listeners = new Set<() => void>();
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches,
      media: query,
      onchange: null,
      addEventListener: (_: string, cb: () => void) => listeners.add(cb),
      removeEventListener: (_: string, cb: () => void) => listeners.delete(cb),
      addListener: (cb: () => void) => listeners.add(cb),
      removeListener: (cb: () => void) => listeners.delete(cb),
      dispatchEvent: () => true,
    })) as unknown as typeof window.matchMedia;
    return { listeners, emit: () => listeners.forEach((cb) => cb()) };
  }

  function storageWith(theme: Theme | null) {
    return { getItem: () => theme, setItem: () => undefined };
  }

  it('reads the theme key from localStorage', () => {
    const spy = { getItem: vi.fn(() => 'dark'), setItem: () => undefined };
    expect(resolveInitialTheme(spy)).toBe('dark');
    expect(spy.getItem).toHaveBeenCalledWith(THEME_STORAGE_KEY);
  });

  it('uses the system preference as a fallback, defaulting to light', () => {
    fakeSystem('dark');
    expect(resolveInitialTheme(noStorage())).toBe('dark');
    fakeSystem('light');
    expect(resolveInitialTheme(noStorage())).toBe('light');
    fakeSystem(null);
    expect(resolveInitialTheme(noStorage())).toBe('light');
  });

  it('inverts the previous theme when toggling and marks it manual', () => {
    expect(useThemeStore.getState().theme).toBe('light');
    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().theme).toBe('dark');
    expect(useThemeStore.getState().hasManualOverride).toBe(true);
  });

  it('followSystem never clobbers a stored manual override', () => {
    useThemeStore.getState().init(storageWith('light'), () => 'light');
    useThemeStore.getState().followSystem('dark');
    expect(useThemeStore.getState().theme).toBe('light');
    expect(useThemeStore.getState().resolvedTheme).toBe('light');
  });

  it('followSystem tracks the system scheme when nothing is stored', () => {
    useThemeStore.getState().init(noStorage(), () => 'light');
    useThemeStore.getState().followSystem('dark');
    expect(useThemeStore.getState().theme).toBe('dark');
    expect(useThemeStore.getState().resolvedTheme).toBe('dark');
    expect(useThemeStore.getState().hasManualOverride).toBe(false);
  });
});

describe('ThemeToggle (visible theme control, A9)', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    window.matchMedia = vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    })) as unknown as typeof window.matchMedia;
    localStorage.clear();
    useThemeStore.setState({ theme: 'light', resolvedTheme: 'light' });
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
    localStorage.clear();
    useThemeStore.setState({ theme: 'light', resolvedTheme: 'light' });
  });

  it('renders a toggle button and a polite status live region', () => {
    render(
      <ThemeProvider>
        <ThemeToggle />
      </ThemeProvider>,
    );
    expect(
      screen.getByRole('button', { name: /switch to dark theme/i }),
    ).toBeTruthy();
    const status = document.querySelector('[role="status"]');
    expect(status).toBeTruthy();
    expect(status?.getAttribute('aria-live')).toBe('polite');
  });

  it('announces the change and flips data-theme + storage', () => {
    render(
      <ThemeProvider>
        <ThemeToggle />
      </ThemeProvider>,
    );
    const status = document.querySelector('[role="status"]');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');

    fireEvent.click(
      screen.getByRole('button', { name: /switch to dark theme/i }),
    );

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(status?.textContent).toContain('dark');
    expect(
      screen.getByRole('button', { name: /switch to light theme/i }),
    ).toBeTruthy();
  });
});
