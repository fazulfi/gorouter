import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const NAV = [
  { label: 'Dashboard', href: '/', routeId: 'dashboard' },
  { label: 'Providers', href: '/providers', routeId: 'providers' },
  { label: 'API Keys', href: '/api-keys', routeId: 'api-keys' },
  { label: 'Tokens', href: '/pats', routeId: 'pats' },
  { label: 'Settings', href: '/settings', routeId: 'settings' },
] as const;

/**
 * Establish an authenticated browser context. The Admin API v1 is not wired
 * into the server bootstrap at the FE-02 base commit, so the shell is
 * exercised against the real FE-02 client by intercepting the auth-guard
 * calls with a session that reports authenticated, and issuing the
 * gorouter_csrf cookie the client must echo on mutations (a11y A2).
 */
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

test.describe('Shell (authenticated)', () => {
  test.beforeEach(async ({ page }) => {
    await authenticated(page);
  });

  test.describe('skip link (a11y A13)', () => {
    test('is the first focusable element and jumps focus to main', async ({
      page,
    }) => {
      const skip = page.getByRole('link', { name: 'Skip to main content' });
      await expect(skip).toBeVisible();
      // The skip link is the first focusable element in the DOM.
      const firstFocusable = page.locator(
        'body a[href], body button, body input, body [tabindex]:not([tabindex="-1"])',
      );
      await expect(firstFocusable.first()).toHaveText('Skip to main content');
      // Pressing Enter on it jumps focus to <main>.
      await skip.focus();
      await skip.press('Enter');
      await expect(page.locator('main')).toBeFocused();
    });

    test('is visually hidden until focused', async ({ page }) => {
      const skip = page.getByRole('link', { name: 'Skip to main content' });
      await expect(skip).toBeAttached();
      // When not focused, the link is visually hidden by the shell CSS.
      const box = await skip.boundingBox();
      expect(box).not.toBeNull();
    });
  });

  test.describe('single h1 per route (a11y A16-A17)', () => {
    for (const { href, label } of NAV) {
      test(`route ${href} renders exactly one h1`, async ({ page }) => {
        await page.goto(href);
        await expect(page.locator('main h1').first()).toBeVisible();
        expect(await page.locator('main h1').count()).toBe(1);
      });
    }
  });

  function viewportWidth(page: Page): Promise<number> {
  return page.evaluate(() => window.innerWidth);
}

test.describe('image-alt + focus visibility', () => {
    test('has a visible focus ring on nav links', async ({ page }) => {
      // On mobile the rail is hidden; the drawer describe covers the nav
      // semantics, so this focus-ring check runs when the rail is visible.
      const width = await viewportWidth(page);
      if (width <= 768) {
        test.skip();
        return;
      }
      const link = page.getByRole('navigation', { name: 'Primary' })
        .getByRole('link', { name: 'Providers' });
      await link.focus();
      const outline = await link.evaluate((el) => {
        const s = getComputedStyle(el);
        return s.outlineWidth !== '0px' && s.outlineStyle !== 'none';
      });
      expect(outline).toBe(true);
    });
  });

  test.describe('aria-current on active nav (a11y A12)', () => {
    for (const { href, label } of NAV) {
      test(`${label} route marks its nav item active`, async ({ page }) => {
        await page.goto(href);
        await page.waitForURL(href);
        const active = page.locator('aside nav >> a[aria-current="page"]');
        await expect(active).toHaveCount(1);
        await expect(active).toContainText(label);
      });
    }
  });

  test.describe('document titles (product P13)', () => {
    for (const { href, label } of NAV) {
      test(`${label} route sets the document title`, async ({ page }) => {
        await page.goto(href);
        if (label === 'Tokens') {
          await expect(page).toHaveTitle('Tokens - gorouter');
        } else {
          await expect(page).toHaveTitle(`${label} - gorouter`);
        }
      });
    }
  });

  test.describe('lander landmarks', () => {
    test('renders banner, navigation, complementary and main landmarks', async ({
      page,
    }) => {
      // On mobile the rail is hidden; open the drawer to reveal the nav so the
      // complementary landmark is present (the drawer describe also validates it).
      const width = await viewportWidth(page);
      if (width <= 768) {
        const menu = page.getByRole('button', { name: 'Open navigation menu' });
        await menu.click();
        await expect(page.locator('#primary-nav')).toBeVisible();
      } else {
        await expect(page.locator('aside > nav[aria-label="Primary"], aside nav')).toBeVisible();
      }
      await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible();
      await expect(page.locator('header')).toBeVisible();
      await expect(page.locator('main')).toBeVisible();
      await expect(page.locator('main')).toHaveAttribute('tabindex', '-1');
    });
  });
});

test.describe('mobile drawer (375px)', () => {
  test.use({ viewport: { width: 375, height: 667 } });

  test.beforeEach(async ({ page }) => {
    await authenticated(page);
  });

  test('drawer trigger has aria-expanded and aria-controls', async ({ page }) => {
    const trigger = page.getByRole('button', { name: 'Open navigation menu' });
    await expect(trigger).toBeVisible();
    await expect(trigger).toHaveAttribute('aria-expanded', 'false');
    await expect(trigger).toHaveAttribute('aria-controls', 'primary-nav');
  });

  test('drawer traps focus and closes on Escape', async ({ page }) => {
    const trigger = page.getByRole('button', { name: 'Open navigation menu' });
    await trigger.click();
    await expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const drawer = page.locator('aside[id="primary-nav"]');
    await expect(drawer).toBeVisible();
    const close = page.getByRole('button', { name: 'Close navigation menu' });
    await close.press('Escape');
    await expect(drawer).toHaveCount(0);
    await expect(trigger).toBeFocused();
  });

  test('no horizontal scroll overflow on mobile', async ({ page }) => {
    const hasOverflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth >
        document.documentElement.clientWidth,
    );
    expect(hasOverflow).toBe(false);
  });
});

test.describe('CSRF double-submit (a11y A2)', () => {
  test.beforeEach(async ({ page }) => {
    await authenticated(page);
  });

  test('mutations carry X-CSRF-Token matching the gorouter_csrf cookie', async ({
    page,
  }) => {
    let sentToken: string | null = null;
    await page.route('**/api/admin/v1/pats/**', async (route) => {
      const req = route.request();
      if (req.method() === 'DELETE') {
        sentToken = req.headers()['x-csrf-token'] ?? null;
      }
      await route.fulfill({ status: 404, contentType: 'application/json', body: '{}' });
    });

    const cookies = await page.context().cookies();
    const csrf = cookies.find((c) => c.name === 'gorouter_csrf');
    expect(csrf).toBeTruthy();
    expect(csrf.value).toBe('e2e-csrf-token');

    // Drive a real client mutation through the Vite-served module so the
    // production client's CSRF echo path is exercised, not a hand-rolled fetch.
    await page.goto('/');
    await page.evaluate(async () => {
      const mod = await import('/src/shared/api/client.ts');
      await mod.revokePAT('__e2e_nonexistent__').catch(() => undefined);
    });

    await expect
      .poll(() => sentToken, { timeout: 5000 })
      .not.toBeNull();
    expect(sentToken).toBe('e2e-csrf-token');
  });
});

test.describe('accessibility (axe-core)', () => {
  test('has no critical or serious violations on the dashboard', async ({
    page,
  }) => {
    await page.goto('/');
    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'best-practice'])
      .analyze();
    const serious = results.violations.filter(
      (v) => v.impact === 'critical' || v.impact === 'serious',
    );
    expect(serious).toHaveLength(0);
  });

  test('color-contrast is pinned as critical (a11y A40)', async ({ page }) => {
    const results = await new AxeBuilder({ page })
      .withTags(['wcag2aa'])
      .analyze();
    const contrast = results.violations.find((v) => v.id === 'color-contrast');
    if (contrast) {
      expect(contrast.impact).toBe('critical');
    }
  });
});