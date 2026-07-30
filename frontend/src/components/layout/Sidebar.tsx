import styles from './Sidebar.module.css';

const navItems = [
  { label: 'Dashboard', icon: '⊞', href: '/' },
  { label: 'Providers', icon: '⇌', href: '/providers' },
  { label: 'API Keys', icon: '🔑', href: '/api-keys' },
  { label: 'Tokens', icon: '📋', href: '/pats' },
  { label: 'Settings', icon: '⚙', href: '/settings' },
];

export default function Sidebar() {
  return (
    <aside className={styles.sidebar}>
      <div className={styles.logo}>gorouter</div>
      <nav className={styles.nav}>
        {navItems.map((item) => (
          <a key={item.href} href={item.href} className={styles.link} aria-label={item.label}>
            <span className={styles.icon} aria-hidden="true">{item.icon}</span>
            {item.label}
          </a>
        ))}
      </nav>
    </aside>
  );
}
