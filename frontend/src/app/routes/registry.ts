// Route registry (FE-02 owns the root shell + auth routes). Feature atoms
// register additional routes under `src/app/routes/` by importing and calling
// `registerFeatureRoutes` during their feature atom; each route is lazily
// code-split and carries a document title + breadcrumb label.
import type { RouteObject } from 'react-router-dom';

export interface RouteMeta {
  title: string;
  breadcrumb: string;
}

export type RegisteredRoute = RouteObject & {
  routeId?: string;
  meta?: RouteMeta;
};

let featureRoutes: RegisteredRoute[] = [];

const SHELL_ROUTES: RegisteredRoute[] = [
  {
    routeId: 'dashboard',
    path: '/',
    meta: { title: 'Dashboard', breadcrumb: 'Dashboard' },
  },
  {
    routeId: 'providers',
    path: '/providers',
    meta: { title: 'Providers', breadcrumb: 'Providers' },
  },
  {
    routeId: 'api-keys',
    path: '/api-keys',
    meta: { title: 'API Keys', breadcrumb: 'API Keys' },
  },
  {
    routeId: 'pats',
    path: '/pats',
    meta: { title: 'Tokens', breadcrumb: 'Tokens' },
  },
  {
    routeId: 'settings',
    path: '/settings',
    meta: { title: 'Settings', breadcrumb: 'Settings' },
  },
];

/**
 * Register feature routes (called exactly once per feature atom). Kept mutable
 * so later feature atoms append to the shared shell router without re-editing
 * the root tree owned here.
 */
export function registerFeatureRoutes(routes: RegisteredRoute[]): void {
  featureRoutes = [...featureRoutes, ...routes];
}

export function getFeatureRoutes(): RegisteredRoute[] {
  return featureRoutes;
}

export function getShellRoutes(): RegisteredRoute[] {
  return SHELL_ROUTES;
}
