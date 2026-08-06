import { lazy, Suspense, type ReactElement } from 'react';
import { createBrowserRouter, type RouteObject } from 'react-router-dom';
import Shell from '@/app/components/Shell';
import AuthGuard from '@/app/components/AuthGuard';
import Page from '@/app/components/Page';
import PageState from '@/app/components/PageState';
import LoginPage from '@/app/routes/LoginPage';
import { getShellRoutes, getFeatureRoutes } from '@/app/routes/registry';

function lazyPlaceholder(title: string): ReactElement {
  const Comp = lazy(() =>
    import('@/app/routes/PlaceholderPage').then((m) => ({
      default: () => m.default({ title }),
    })),
  );
  return (
    <PageState kind="data">
      <Suspense fallback={<PageState kind="loading" />}>
        <Comp />
      </Suspense>
    </PageState>
  );
}

/**
 * Compose the browser router: a public /login route, the authenticated shell
 * guarded by AuthGuard wrapping <Shell /> with per-feature code-split lazy
 * routes, and an explicit not-found fallback inside the shell.
 */
export function createAppRouter() {
  const children: RouteObject[] = [];

  for (const route of getShellRoutes()) {
    const title = route.meta?.title ?? 'Dashboard';
    children.push({
      path: String(route.path),
      element: lazyPlaceholder(title),
    });
  }

  for (const route of getFeatureRoutes()) {
    if (route.element && route.path) {
      children.push({ path: String(route.path), element: route.element });
    }
  }

  children.push({
    path: '*',
    element: (
      <Page title="Page not found" description="The page you requested does not exist." />
    ),
  });

  return createBrowserRouter([
    { path: '/login', element: <LoginPage /> },
    {
      element: (
        <AuthGuard>
          <Shell />
        </AuthGuard>
      ),
      children,
    },
  ]);
}
