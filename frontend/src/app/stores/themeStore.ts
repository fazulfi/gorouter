import { create } from 'zustand';

export const THEME_STORAGE_KEY = 'theme';

export type Theme = 'light' | 'dark';

type MaybeStorage = Pick<Storage, 'getItem' | 'setItem'> | null | undefined;

function defaultSystemTheme(): Theme {
  if (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  ) {
    return 'dark';
  }
  return 'light';
}

export function readStoredTheme(storage: MaybeStorage): Theme | null {
  if (!storage) return null;
  try {
    const raw = storage.getItem(THEME_STORAGE_KEY);
    return raw === 'dark' || raw === 'light' ? raw : null;
  } catch {
    return null;
  }
}

export function resolveInitialTheme(
  storage: MaybeStorage,
  systemTheme: () => Theme = defaultSystemTheme,
): Theme {
  return readStoredTheme(storage) ?? systemTheme();
}

interface ThemeStore {
  theme: Theme;
  resolvedTheme: Theme;
  hasManualOverride: boolean;
  init: (storage: MaybeStorage, systemTheme: () => Theme) => void;
  toggle: () => void;
  followSystem: (scheme: Theme) => void;
}

export const useThemeStore = create<ThemeStore>((set) => ({
  theme: 'light',
  resolvedTheme: 'light',
  hasManualOverride: false,

  init: (storage, systemTheme) => {
    const stored = readStoredTheme(storage);
    const initial = stored ?? systemTheme();
    set({
      theme: stored ?? initial,
      resolvedTheme: initial,
      hasManualOverride: stored !== null,
    });
  },

  toggle: () => {
    const next: Theme = useThemeStore.getState().theme === 'dark' ? 'light' : 'dark';
    set({ theme: next, resolvedTheme: next, hasManualOverride: true });
  },

  followSystem: (scheme) => {
    const { hasManualOverride } = useThemeStore.getState();
    if (hasManualOverride) return;
    set({ theme: scheme, resolvedTheme: scheme });
  },
}));
