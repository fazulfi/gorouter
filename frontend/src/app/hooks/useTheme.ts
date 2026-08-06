import { useContext } from 'react';
import { ThemeContext } from '@/app/providers/theme';
import type { Theme } from '@/app/stores/themeStore';

export interface UseThemeResult {
  theme: Theme;
  resolvedTheme: Theme;
  toggle: () => void;
}

export function useTheme(): UseThemeResult {
  const ctx = useContext(ThemeContext);
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return ctx;
}
