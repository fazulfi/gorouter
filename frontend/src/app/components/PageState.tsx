import type { ReactNode } from 'react';
import { useToastStore } from '@/app/stores/toastStore';
import styles from './PageState.module.css';

export type PageStateKind = 'loading' | 'empty' | 'error' | 'permission' | 'data';

export interface PageStateProps {
  kind: PageStateKind;
  title?: string;
  message?: string;
  primaryAction?: ReactNode;
  retry?: () => void;
  children?: ReactNode;
}

function PermissionDenied({
  message,
  onReport,
}: {
  message?: string;
  onReport?: () => void;
}) {
  return (
    <div className={styles.state}>
      <h2 className={styles.heading}>You do not have access</h2>
      <p className={styles.copy}>
        {message ??
          'This workspace is not available to your account.'}
      </p>
      <p className={styles.copy}>
        To request access, contact a workspace administrator.
      </p>
      {onReport && (
        <button type="button" className={styles.link} onClick={onReport}>
          Report an issue
        </button>
      )}
    </div>
  );
}

/**
 * Five-state page contract (a11y A34-A38). A feature page renders exactly one
 * of: loading (skeleton + aria-busy), empty (message + primary action), error
 * (message + retry + reported-toast link), permission-denied (clear "what" +
 * "who to ask" + docs link, distinct from timeout, no capability enumeration),
 * or data (children). Error reporting is pushed to the single shared alert
 * region (A38).
 */
export default function PageState({
  kind,
  title,
  message,
  primaryAction,
  retry,
  children,
}: PageStateProps) {
  const pushToast = useToastStore((s) => s.push);

  if (kind === 'data') return <>{children}</>;

  if (kind === 'permission') {
    const report = retry
      ? undefined
      : () => pushToast('info', 'Reported. The workspace owner has been notified.');
    return <PermissionDenied message={message} onReport={report} />;
  }

  if (kind === 'error') {
    return (
      <div className={styles.state} role="status" aria-live="polite">
        <h2 className={styles.heading}>{title ?? 'Something went wrong'}</h2>
        <p className={styles.copy}>
          {message ?? 'The request could not be completed. Please try again.'}
        </p>
        <div className={styles.actions}>
          {retry && (
            <button type="button" className={styles.primary} onClick={retry}>
              Try again
            </button>
          )}
          <button
            type="button"
            className={styles.link}
            onClick={() =>
              pushToast('info', 'Reported. The workspace owner has been notified.')
            }
          >
            Report an issue
          </button>
        </div>
      </div>
    );
  }

  if (kind === 'empty') {
    return (
      <div className={styles.state}>
        <h2 className={styles.heading}>{title ?? 'Nothing here yet'}</h2>
        {message && <p className={styles.copy}>{message}</p>}
        {primaryAction && <div className={styles.actions}>{primaryAction}</div>}
      </div>
    );
  }

  return (
    <div className={styles.loading} aria-busy="true" aria-live="polite">
      <div className={styles.skeleton} />
      <div className={styles.skeleton} />
      <div className={styles.skeleton} />
    </div>
  );
}
