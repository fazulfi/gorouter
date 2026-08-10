import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const ROUTES = [
  { path: '/', name: 'Dashboard' },
  { path: '/providers', name: 'Providers' },
  { path: '/api-keys', name: 'API Keys' },
  { path: '/pats', name: 'Tokens' },
  { path: '/settings', name: 'Settings' },
] as const;

const VIEWPORTS = [
  { name: 'mobile', width: 375, height: 667 },
  { name: 'tablet', width: 768, height: 1024 },
  { name: 'desktop', width: 1280, height: 720 },
] as const;

async function authenticated(page: Page): Promise<void> {
  await page.addInitScript(() => {
    document.cookie = 'gorouter_csrf=e2e-csrf-token; Path=/';
  });
  await page.route('**/api/admin/v1/auth/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        authenticated: true,
        user: { id: 'u1', email: 'admin@gorouter.local', is_admin: true },
      }),
    }),
  );
  await page.route('**/api/admin/v1/auth/me', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ id: 'u1', email: 'admin@gorouter.local', is_admin: true }),
    }),
  );
}

async function openRoute(page: Page, path: string, width: number): Promise<void> {
  await page.setViewportSize({ width, height: width < 800 ? 1024 : 720 });
  await page.goto(path);
  await expect(page.locator('main h1')).toHaveCount(1);
  if (width < 800) {
    const menu = page.getByRole('button', { name: 'Open navigation menu' });
    await expect(menu).toBeVisible();
    await menu.click();
    await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible();
  }
}

test.describe('P5-T11 WCAG 2.2 AA route matrix', () => {
  test.beforeEach(async ({ page }) => {
    await authenticated(page);
  });

  for (const viewport of VIEWPORTS) {
    for (const route of ROUTES) {
      test(`${viewport.name} ${route.name} has no critical or serious axe violations`, async ({ page }) => {
        await openRoute(page, route.path, viewport.width);
        const results = await new AxeBuilder({ page })
          .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice'])
          .analyze();
        const serious = results.violations.filter((violation) =>
          violation.impact === 'critical' || violation.impact === 'serious',
        );
        expect(serious, JSON.stringify(serious, null, 2)).toHaveLength(0);
      });
    }
  }

  for (const theme of ['light', 'dark'] as const) {
    for (const route of ROUTES) {
      test(`${theme} ${route.name} keeps landmarks and accessible names`, async ({ page }) => {
        await page.addInitScript((value) => localStorage.setItem('theme', value), theme);
        await openRoute(page, route.path, 1280);
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(page.locator('header')).toBeVisible();
        await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible();
        await expect(page.getByRole('main')).toBeVisible();
        await expect(page.getByRole('button', { name: /switch to/i })).toBeVisible();
        await expect(page.getByRole('link', { name: 'Skip to main content' })).toBeAttached();
        await expect(page.locator('img[alt=""]')).toHaveCount(0);
      });
    }
  }

  test('keyboard path exposes skip link, focus ring, and no mobile trap', async ({ page }) => {
    await authenticated(page);
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/');
    const skip = page.getByRole('link', { name: 'Skip to main content' });
    await skip.focus();
    await skip.press('Enter');
    await expect(page.locator('main')).toBeFocused();

    const menu = page.getByRole('button', { name: 'Open navigation menu' });
    await menu.focus();
    await menu.click();
    const drawer = page.locator('aside[id="primary-nav"]');
    await expect(drawer).toBeVisible();
    await page.getByRole('button', { name: 'Close navigation menu' }).press('Escape');
    await expect(drawer).toHaveCount(0);
    await expect(menu).toBeFocused();
  });

  test('screen-reader status and page semantics remain exposed', async ({ page }) => {
    await openRoute(page, '/settings', 1280);
    await expect(page.locator('[role="status"][aria-live="polite"]').first()).toBeVisible();
    await expect(page.locator('main h1')).toHaveText('Settings');
    await expect(page.locator('aside nav a[aria-current="page"]')).toHaveText('Settings');
  });
});
