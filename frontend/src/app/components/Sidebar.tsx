import { NavLink, useLocation } from 'react-router-dom';
import { NAV_ITEMS } from '@/app/navigation';
import styles from './Sidebar.module.css';

/**
 * Primary navigation landmark. Renders <nav> with the five shell destinations;
 * the active item carries aria-current="page" (a11y A12). Rendered either as
 * the fixed desktop rail or inside the mobile drawer <aside>.
 */
export default function Sidebar() {
  const location = useLocation();
  const pathname = location.pathname;

  return (
    <nav aria-label="Primary" className={styles.nav}>
      {NAV_ITEMS.map((item) => {
        const isActive =
          item.href === '/' ? pathname === '/' : pathname.startsWith(item.href);
        return (
          <NavLink
            key={item.routeId}
            to={item.href}
            end={item.href === '/'}
            aria-current={isActive ? 'page' : undefined}
            className={isActive ? `${styles.link} ${styles.active}` : styles.link}
          >
            {item.label}
          </NavLink>
        );
      })}
    </nav>
  );
}
