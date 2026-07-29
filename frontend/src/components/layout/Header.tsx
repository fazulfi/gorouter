import styles from './Header.module.css';

export default function Header() {
  return (
    <header className={styles.header}>
      <div className={styles.title}>Dashboard</div>
      <div className={styles.actions}>
        <span className={styles.statusDot} title="Connected" />
      </div>
    </header>
  );
}
