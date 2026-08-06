import { createContext, useEffect, useMemo, type ReactNode } from 'react';
import {
  useThemeStore,
  THEME_STORAGE_KEY,
  type Theme,
} from '@/app/stores/themeStore';

interface ThemeContextValue {
  theme: Theme;
  resolvedTheme: Theme;
  toggle: () => void;
}

export const ThemeContext = createContext<ThemeContextValue | null>(null);

function systemMatch(): MediaQueryList | null {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return null;
  }
  return window.matchMedia('(prefers-color-scheme: dark)');
}

function applyTheme(theme: string): void {
  document.documentElement.setAttribute('data-theme', theme);
}

function persistTheme(storage: Storage | null | undefined, theme: string): void {
  try {
    storage?.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Storage can throw in private/blocked contexts; the DOM attribute still
    // applies for the session.
  }
}

interface ThemeProviderProps {
  children: ReactNode;
}

/**
 * ThemeProvider (FE-03, D2/A8-A9). Writes the resolved theme to
 * <html data-theme> consumed by Tailwind's `darkMode: 'class'`. A stored
 * manual override wins over the system preference; the matchMedia listener
 * updates the resolved theme without clobbering a stored override.
 */
export default function ThemeProvider({ children }: ThemeProviderProps) {
  const theme = useThemeStore((s) => s.theme);
  const resolvedTheme = useThemeStore((s) => s.resolvedTheme);
  const hasManualOverride = useThemeStore((s) => s.hasManualOverride);
  const toggle = useThemeStore((s) => s.toggle);
  const initStore = useThemeStore((s) => s.init);

  useEffect(() => {
    initStore(window.localStorage, () =>
      systemMatch()?.matches ? 'dark' : 'light',
    );
  }, [initStore]);

  useEffect(() => {
    const mql = systemMatch();
    if (!mql) return;
    const onChange = () => {
      const { followSystem, resolvedTheme: current, hasManualOverride } =
        useThemeStore.getState();
      if (hasManualOverride) return;
      const next = mql.matches ? 'dark' : 'light';
      if (next !== current) followSystem(next);
    };
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, []);

  useEffect(() => {
    if (theme === 'light' || theme === 'dark') {
      applyTheme(theme);
      if (hasManualOverride) persistTheme(window.localStorage, theme);
    }
  }, [theme, hasManualOverride]);

  const value = useMemo(
    () => ({ theme, resolvedTheme, toggle }),
    [theme, resolvedTheme, toggle],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}
