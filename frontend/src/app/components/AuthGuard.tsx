import { useEffect, useState, type ReactNode } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { useAuthStore } from '@/app/stores/authStore';

interface AuthGuardProps {
  children?: ReactNode;
  fallback?: ReactNode;
}

/**
 * Route guard (a11y A44): unauthenticated visits redirect to /login?next=...
 * carrying the requested path so the login flow returns the user to their
 * deep link. Authenticated deep links survive a full reload because the
 * embedded SPA fallback serves index.html for non-API GET and this guard
 * re-validates the session on mount.
 */
export default function AuthGuard({ children, fallback }: AuthGuardProps) {
  const location = useLocation();
  const state = useAuthStore((s) => s.state);
  const check = useAuthStore((s) => s.check);

  const [checked, setChecked] = useState(false);

  useEffect(() => {
    let cancelled = false;
    check().finally(() => {
      if (!cancelled) setChecked(true);
    });
    return () => {
      cancelled = true;
    };
  }, [check, location.pathname]);

  if (!checked || state === 'loading') {
    return fallback ?? null;
  }

  if (state === 'unauthenticated') {
    const next = `${location.pathname}${location.search}`;
    return <Navigate to={`/login?next=${encodeURIComponent(next)}`} replace />;
  }

  return children ?? <Outlet />;
}
