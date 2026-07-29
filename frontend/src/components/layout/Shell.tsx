import Sidebar from './Sidebar';
import Header from './Header';
import styles from './Shell.module.css';

export default function Shell() {
  return (
    <div className={styles.shell}>
      <Sidebar />
      <div className={styles.mainArea}>
        <Header />
        <main className={styles.content}>
          <p className={styles.placeholder}>Select a view from the sidebar.</p>
        </main>
      </div>
    </div>
  );
}
