import type { HTMLAttributes } from 'react';
import { cn } from '@/lib/utils';
import styles from './separator.module.css';

export interface SeparatorProps extends HTMLAttributes<HTMLDivElement> {
  orientation?: 'horizontal' | 'vertical';
  decorative?: boolean;
}

/**
 * Visual divider matching the Radix Separator API. Decorative separators
 * (the default) carry no ARIA role; non-decorative ones expose
 * role="separator" with an explicit aria-orientation.
 */
export function Separator({
  className,
  orientation = 'horizontal',
  decorative = true,
  ...props
}: SeparatorProps) {
  return (
    <div
      role={decorative ? undefined : 'separator'}
      aria-orientation={decorative ? undefined : orientation}
      data-orientation={orientation}
      className={cn(
        styles.separator,
        orientation === 'vertical' ? styles.vertical : styles.horizontal,
        className,
      )}
      {...props}
    />
  );
}
