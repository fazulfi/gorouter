import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const NAV_LINKS = ['Dashboard', 'Providers', 'API Keys', 'Tokens', 'Settings'] as const;

test.describe('Shell layout', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test.describe('viewport: desktop (1280×720)', () => {
    test.use({ viewport: { width: 1280, height: 720 } });

    test('renders the page title', async ({ page }) => {
      await expect(page).toHaveTitle('gorouter');
    });

    test('shows the sidebar logo', async ({ page }) => {
      await expect(page.getByText('gorouter')).toBeVisible();
    });

    test('shows all five navigation links', async ({ page }) => {
      const links = page.getByRole('link');
      await expect(links).toHaveCount(5);
      for (const label of NAV_LINKS) {
        await expect(page.getByRole('link', { name: label })).toBeVisible();
      }
    });

    test('shows the header with Dashboard title', async ({ page }) => {
      // The header title is inside a <div> (not a link), so use exact match
      await expect(page.getByText('Dashboard', { exact: true })).toBeVisible();
    });

    test('shows the connection status indicator', async ({ page }) => {
      await expect(page.locator('[title="Connected"]')).toBeVisible();
    });

    test('renders the placeholder content message', async ({ page }) => {
      await expect(
        page.getByText('Select a view from the sidebar.'),
      ).toBeVisible();
    });

    test('has semantic landmarks: banner, navigation, complementary, main', async ({
      page,
    }) => {
      await expect(page.locator('header')).toBeVisible();
      await expect(page.locator('nav')).toBeVisible();
      await expect(page.locator('aside')).toBeVisible();
      await expect(page.locator('main')).toBeVisible();
    });

    test('has correct hrefs on navigation links', async ({ page }) => {
      const hrefs = [
        { label: 'Dashboard', href: '/' },
        { label: 'Providers', href: '/providers' },
        { label: 'API Keys', href: '/api-keys' },
        { label: 'Tokens', href: '/pats' },
        { label: 'Settings', href: '/settings' },
      ];
      for (const { label, href } of hrefs) {
        const link = page.getByRole('link', { name: label });
        await expect(link).toHaveAttribute('href', href);
      }
    });
  });

  test.describe('viewport: mobile (375×667)', () => {
    test.use({ viewport: { width: 375, height: 667 } });

    test('renders core content on mobile', async ({ page }) => {
      await expect(page.getByText('gorouter')).toBeVisible();
      // Use getByRole to disambiguate the header title from the sidebar link
      const headerTitle = page.locator('header').getByText('Dashboard');
      await expect(headerTitle).toBeVisible();
      await expect(
        page.getByText('Select a view from the sidebar.'),
      ).toBeVisible();
      await expect(page.locator('[title="Connected"]')).toBeVisible();
    });

    test('navigation links are accessible on mobile', async ({ page }) => {
      for (const label of NAV_LINKS) {
        await expect(
          page.getByRole('link', { name: label }),
        ).toBeVisible();
      }
    });

    test('no horizontal scroll overflow on mobile', async ({ page }) => {
      const hasOverflow = await page.evaluate(() => {
        return (
          document.documentElement.scrollWidth >
          document.documentElement.clientWidth
        );
      });
      expect(hasOverflow).toBe(false);
    });
  });

  test.describe('keyboard navigation', () => {
    test.use({ viewport: { width: 1280, height: 720 } });

    test('all navigation links can be focused programmatically', async ({
      page,
    }) => {
      for (const label of NAV_LINKS) {
        const link = page.getByRole('link', { name: label });
        await link.focus();
        await expect(link).toBeFocused();
      }
    });

    test('links are focusable via script (visible focus indicator candidate)', async ({
      page,
    }) => {
      // Only focusable elements (links) should accept programmatic focus
      for (const label of NAV_LINKS) {
        const link = page.getByRole('link', { name: label });
        await link.focus();
        await expect(link).toBeFocused();
      }
    });
  });

  test.describe('accessibility (axe-core)', () => {
    test.use({ viewport: { width: 1280, height: 720 } });

    test('has no serious or critical accessibility violations on desktop', async ({
      page,
    }) => {
      const results = await new AxeBuilder({ page })
        .withTags([
          'wcag2a',
          'wcag2aa',
          'wcag21a',
          'wcag21aa',
          'best-practice',
        ])
        .analyze();
      const serious = results.violations.filter(
        (v) => v.impact === 'critical' || v.impact === 'serious',
      );
      expect(serious).toHaveLength(0);
    });

    test('has no serious or critical accessibility violations in dark mode', async ({
      page,
    }) => {
      await page.emulateMedia({ colorScheme: 'dark' });
      const results = await new AxeBuilder({ page })
        .withTags([
          'wcag2a',
          'wcag2aa',
          'wcag21a',
          'wcag21aa',
          'best-practice',
        ])
        .analyze();
      const serious = results.violations.filter(
        (v) => v.impact === 'critical' || v.impact === 'serious',
      );
      expect(serious).toHaveLength(0);
    });
  });
});
