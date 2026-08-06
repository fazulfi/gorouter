// Sidebar navigation (FE-02 shell). Five root-level destinations map to the
// upstream /dashboard/* surfaces per the route mapping table (a11y A1).
export interface NavItem {
  label: string;
  href: string;
  routeId: string;
}

export const NAV_ITEMS: NavItem[] = [
  { label: 'Dashboard', href: '/', routeId: 'dashboard' },
  { label: 'Providers', href: '/providers', routeId: 'providers' },
  { label: 'API Keys', href: '/api-keys', routeId: 'api-keys' },
  { label: 'Tokens', href: '/pats', routeId: 'pats' },
  { label: 'Settings', href: '/settings', routeId: 'settings' },
];
