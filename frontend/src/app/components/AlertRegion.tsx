import { useEffect } from 'react';
import { useToastStore } from '@/app/stores/toastStore';
import styles from './AlertRegion.module.css';

/**
 * Single shared role="alert" mount (a11y A38): one live-region host for all
 * error toasts, mounted once at the shell root so no feature duplicates nested
 * announcements. Auto-dismiss removes the toast; the live region stays mounted.
 */
export default function AlertRegion() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);

  useEffect(() => {
    if (toasts.length === 0) return;
    const timers = toasts.map((t) =>
      setTimeout(() => dismiss(t.id), 8000),
    );
    return () => timers.forEach(clearTimeout);
  }, [toasts, dismiss]);

  return (
    <div className={styles.region} role="alert" aria-live="assertive">
      {toasts.map((t) => (
        <div key={t.id} className={`${styles.toast} ${styles[t.kind]}`}>
          <span className={styles.message}>{t.message}</span>
          <button
            type="button"
            className={styles.dismiss}
            onClick={() => dismiss(t.id)}
            aria-label="Dismiss notification"
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
}
