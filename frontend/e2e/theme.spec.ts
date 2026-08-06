import { test, expect, type Page } from '@playwright/test';

/**
 * Theme system e2e (FE-03, D2/A8-A9): a stored manual override survives a
 * reload and wins over the system scheme; a system scheme change never
 * clobbers a stored manual override; the visible header toggle flips
 * data-theme and announces the change via role="status"/aria-live="polite".
 */
const THEME_KEY = 'theme';

async function authenticated(page: Page): Promise<void> {
  await page.addInitScript(() => {
    document.cookie = 'gorouter_csrf=e2e-csrf-token; Path=/';
  });
  await page.route('**/api/admin/v1/auth/status', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        authenticated: true,
        user: { id: 'u1', email: 'admin@gorouter.local', is_admin: true },
      }),
    });
  });
  await page.route('**/api/admin/v1/auth/me', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'u1',
        email: 'admin@gorouter.local',
        is_admin: true,
      }),
    });
  });
  await page.goto('/');
}

test.describe('theme persistence (FE-03, D2)', () => {
  test.beforeEach(async ({ page }) => {
    await authenticated(page);
  });

  test('manual toggle persists across reload', async ({ page }) => {
    const toggle = page.getByRole('button', { name: /switch to dark theme/i });
    await expect(toggle).toBeVisible();
    await toggle.click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    expect(
      await page.evaluate((key) => localStorage.getItem(key), THEME_KEY),
    ).toBe('dark');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(
      page.getByRole('button', { name: /switch to light theme/i }),
    ).toBeVisible();
  });

  test('stored manual override wins on load', async ({ page }) => {
    await page.addInitScript(
      (key) => localStorage.setItem(key, 'dark'),
      THEME_KEY,
    );
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
});

test.describe('theme system-change listener (FE-03, D2)', () => {
  test('does not clobber a stored manual override', async ({ page }) => {
    await page.addInitScript((key) => localStorage.setItem(key, 'light'), THEME_KEY);
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

    await page.emulateMedia({ colorScheme: 'dark' });
    await page.waitForTimeout(200);
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    expect(
      await page.evaluate((key) => localStorage.getItem(key), THEME_KEY),
    ).toBe('light');
  });

  test('follows the system scheme when no override is stored', async ({
    page,
  }) => {
    await page.goto('/');
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
});

test.describe('theme control announcement (FE-03, A8-A9)', () => {
  test('visible header control announces change via polite live region', async ({
    page,
  }) => {
    await authenticated(page);
    const toggle = page.getByRole('button', { name: /switch to dark theme/i });
    const status = page.locator('[role="status"]', { hasText: /theme/i });
    await expect(status).toHaveAttribute('aria-live', 'polite');

    await toggle.click();
    await expect(status).toContainText('dark');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
});
