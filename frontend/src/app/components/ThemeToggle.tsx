import type { ReactNode } from 'react';
import { useThemeStore } from '@/app/stores/themeStore';
import styles from './ThemeToggle.module.css';

function srLabel(theme: 'light' | 'dark'): string {
  return theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme';
}

function glyph(theme: 'light' | 'dark'): string {
  return theme === 'dark' ? '☀' : '☾';
}

function announcement(theme: 'light' | 'dark'): string {
  return theme === 'dark' ? 'Theme switched to dark.' : 'Theme switched to light.';
}

interface ThemeToggleProps {
  labelPrefix?: string;
}

/**
 * Visible header theme control (FE-03, A9): toggles the manual theme override,
 * announces the change via role="status" aria-live="polite" without stealing
 * focus, and keeps the button label aligned with the action it performs.
 */
export default function ThemeToggle({ labelPrefix }: ThemeToggleProps) {
  const theme = useThemeStore((s) => s.theme);
  const toggle = useThemeStore((s) => s.toggle);
  const label = srLabel(theme);
  const fullLabel = labelPrefix ? `${labelPrefix} ${label}` : label;
  const statusNode: ReactNode | null = <span key={theme}>{announcement(theme)}</span>;

  return (
    <div className={styles.wrap}>
      <button
        type="button"
        className={styles.toggle}
        onClick={toggle}
        aria-label={fullLabel}
      >
        <span aria-hidden="true" className={styles.glyph}>
          {glyph(theme)}
        </span>
      </button>
      <span className={styles.status} role="status" aria-live="polite">
        {statusNode}
      </span>
    </div>
  );
}
