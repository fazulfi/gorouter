import { useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import Sidebar from '@/app/components/Sidebar';
import Header from '@/app/components/Header';
import Drawer from '@/app/components/Drawer';
import AlertRegion from '@/app/components/AlertRegion';
import styles from './Shell.module.css';

/**
 * Authenticated shell (a11y A13, A16-A17). Renders <aside>+<nav>+<header>+
 * <main tabindex="-1">. A visually-hidden-until-focused skip link is the first
 * focusable element and jumps focus to main. Each page renders a single h1
 * inside <main>; heading order matches visual order. One shared role="alert"
 * mount hosts error toasts (A38).
 */
export default function Shell() {
  const location = useLocation();
  const mainRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    document.title = titleFor(deriveTitle(location.pathname));
  }, [location.pathname]);

  const skip = (e: { preventDefault: () => void }) => {
    e.preventDefault();
    mainRef.current?.focus();
  };

  return (
    <div className={styles.shell}>
      <AlertRegion />
      <a className={styles.skipLink} href="#main" onClick={skip}>
        Skip to main content
      </a>
      <aside className={styles.sidebar}>
        <span className={styles.logo}>gorouter</span>
        <Sidebar />
      </aside>
      <div className={styles.mainArea}>
        <div className={styles.mobileBar}>
          <Drawer />
        </div>
        <Header pathname={location.pathname} />
        <main id="main" ref={mainRef} tabIndex={-1} className={styles.content}>
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function deriveTitle(pathname: string): string {
  if (pathname === '/providers') return 'Providers';
  if (pathname === '/api-keys') return 'API Keys';
  if (pathname === '/pats') return 'Tokens';
  if (pathname === '/settings') return 'Settings';
  return 'Dashboard';
}

function titleFor(page: string): string {
  return `${page} - gorouter`;
}
