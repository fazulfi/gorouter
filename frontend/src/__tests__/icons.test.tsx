import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { Icons } from '@/components/icons';

const REQUIRED_KEYS = [
  'logo',
  'spinner',
  'gitHub',
  'google',
  'microsoft',
  'barChart',
  'dollarSign',
  'users',
  'alertCircle',
  'arrowUpRight',
  'arrowDownRight',
] as const;

describe('Icons', () => {
  it('exports every icon referenced by app pages', () => {
    for (const key of REQUIRED_KEYS) {
      expect(Icons[key]).toBeTruthy();
    }
  });

  it('renders SVGs that are hidden from assistive tech by default', () => {
    for (const key of REQUIRED_KEYS) {
      const Icon = Icons[key];
      const { container, unmount } = render(<Icon />);
      const svg = container.querySelector('svg');
      expect(svg).toBeTruthy();
      expect(svg?.getAttribute('aria-hidden')).toBe('true');
      expect(svg?.getAttribute('focusable')).toBe('false');
      unmount();
    }
  });

  it('forwards className onto the svg element', () => {
    const { container } = render(<Icons.logo className="brand-mark" />);
    const svg = container.querySelector('svg');
    expect(svg?.classList.contains('brand-mark')).toBe(true);
  });

  it('carries a built-in spin animation for the spinner', () => {
    const { container } = render(<Icons.spinner />);
    const svg = container.querySelector('svg');
    const classNames = Array.from(svg?.classList ?? []);
    expect(classNames.some((c) => c.includes('spin'))).toBe(true);
  });
});
