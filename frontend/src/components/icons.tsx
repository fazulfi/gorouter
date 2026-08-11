import type { SVGProps } from 'react';
import {
  AlertCircle,
  ArrowDownRight,
  ArrowUpRight,
  BarChart3,
  DollarSign,
  Loader2,
  Users,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import styles from './icons.module.css';

export type IconProps = SVGProps<SVGSVGElement>;

/**
 * Central icon registry consumed by app pages. Every icon renders as an
 * SVG that is hidden from assistive technology by default (icons are
 * decorative next to text labels) and inherits `currentColor` so it
 * follows the active theme.
 */
export const Icons = {
  logo: (props: IconProps) => (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      focusable="false"
      {...props}
    >
      <rect x="3" y="13" width="18" height="7" rx="2" />
      <path d="M12 13V9" />
      <circle cx="12" cy="6" r="2" />
      <path d="M7.5 16.5h.01" />
      <path d="M12 16.5h.01" />
      <path d="M16.5 16.5h.01" />
    </svg>
  ),
  spinner: ({ className, ...props }: IconProps) => (
    <Loader2
      aria-hidden
      focusable="false"
      className={cn(styles.spin, className)}
      {...props}
    />
  ),
  gitHub: (props: IconProps) => (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden focusable="false" {...props}>
      <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
    </svg>
  ),
  google: (props: IconProps) => (
    <svg viewBox="0 0 24 24" aria-hidden focusable="false" {...props}>
      <path
        fill="#EA4335"
        d="M12 5.04c1.61 0 3.06.55 4.2 1.64l3.12-3.12C17.45 1.8 14.97.75 12 .75 7.62.75 3.84 3.27 2 6.94l3.66 2.84C6.53 7.14 9.03 5.04 12 5.04z"
      />
      <path
        fill="#4285F4"
        d="M23.25 12.27c0-.92-.08-1.6-.26-2.31H12v4.19h6.44c-.13 1.08-.83 2.7-2.39 3.79l3.57 2.77c2.14-1.97 3.63-4.88 3.63-8.44z"
      />
      <path
        fill="#FBBC05"
        d="M5.67 14.22a7.03 7.03 0 0 1 0-4.44L2 6.94a11.26 11.26 0 0 0 0 10.12l3.67-2.84z"
      />
      <path
        fill="#34A853"
        d="M12 23.25c3.04 0 5.59-1 7.45-2.72l-3.57-2.77c-.95.66-2.23 1.13-3.88 1.13-2.97 0-5.47-2.1-6.33-4.91L2 16.06c1.84 3.67 5.62 7.19 10 7.19z"
      />
    </svg>
  ),
  microsoft: (props: IconProps) => (
    <svg viewBox="0 0 24 24" aria-hidden focusable="false" {...props}>
      <path fill="#F25022" d="M1 1h10v10H1z" />
      <path fill="#7FBA00" d="M13 1h10v10H13z" />
      <path fill="#00A4EF" d="M1 13h10v10H1z" />
      <path fill="#FFB900" d="M13 13h10v10H13z" />
    </svg>
  ),
  barChart: (props: IconProps) => <BarChart3 aria-hidden focusable="false" {...props} />,
  dollarSign: (props: IconProps) => <DollarSign aria-hidden focusable="false" {...props} />,
  users: (props: IconProps) => <Users aria-hidden focusable="false" {...props} />,
  alertCircle: (props: IconProps) => <AlertCircle aria-hidden focusable="false" {...props} />,
  arrowUpRight: (props: IconProps) => <ArrowUpRight aria-hidden focusable="false" {...props} />,
  arrowDownRight: (props: IconProps) => (
    <ArrowDownRight aria-hidden focusable="false" {...props} />
  ),
};
