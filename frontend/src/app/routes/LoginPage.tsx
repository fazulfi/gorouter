import { useSearchParams } from 'react-router-dom';
import styles from './LoginPage.module.css';

/**
 * Sign-in route placeholder owned in full by FE-07 (P4-T03 login page). FE-02
 * registers the route and renders the public (unauthenticated) shell so the
 * auth guard's /login?next=... redirect resolves; the form arrives later.
 */
export default function LoginPage() {
  const [params] = useSearchParams();
  const next = params.get('next');

  return (
    <main className={styles.login} tabIndex={-1}>
      <div className={styles.card}>
        <h1 className={styles.heading}>Sign in</h1>
        <p className={styles.copy}>
          Welcome back to gorouter. The sign-in form is being set up.
        </p>
        {next && (
          <p className={styles.next}>Returning you to the page you requested.</p>
        )}
      </div>
    </main>
  );
}
