import { NavLink } from 'react-router-dom';
import { NAV_ITEMS } from '@/app/navigation';
import styles from './Header.module.css';

function useRouteCrumb(pathname: string) {
  const match = [...NAV_ITEMS]
    .filter((n) => n.href !== '/')
    .find((n) => pathname.startsWith(n.href));
  if (match) return match.label;
  if (pathname === '/' || pathname === '') return 'Dashboard';
  return 'Dashboard';
}

interface HeaderProps {
  pathname: string;
}

export default function Header({ pathname }: HeaderProps) {
  const crumb = useRouteCrumb(pathname);

  return (
    <header className={styles.header}>
      <nav className={styles.breadcrumb} aria-label="Breadcrumb">
        <NavLink to="/" className={styles.crumbLink}>
          Home
        </NavLink>
        <span className={styles.separator} aria-hidden="true">
          /
        </span>
        <span className={styles.crumbCurrent} aria-current="page">
          {crumb}
        </span>
      </nav>
      <div className={styles.actions}>
        <span className={styles.statusRegion} role="status" aria-live="polite">
          Connected
        </span>
      </div>
    </header>
  );
}
