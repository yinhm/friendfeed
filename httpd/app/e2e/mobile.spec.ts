// @ts-check
import { expect, test, type BrowserContext } from '@playwright/test';

async function authenticate(context: BrowserContext) {
  const baseURL = process.env.E2E_BASE_URL;
  const sessionCookie = process.env.E2E_SESSION_COOKIE;
  if (!baseURL || !sessionCookie) {
    throw new Error('E2E_BASE_URL and E2E_SESSION_COOKIE are required');
  }

  await context.addCookies([
    {
      name: 'ffdbsess',
      value: sessionCookie,
      url: baseURL,
      httpOnly: true,
      sameSite: 'Lax',
    },
  ]);
}

// The 600px breakpoint contract (style.css): on desktop the navigation box
// is always open and its disclosure toggle is hidden; on mobile the toggle
// appears and the sidebar Groups block is hidden (regression guard for the
// mobile Groups fix).
test('sidebar layout switches at the 600px breakpoint', async ({
  context,
  page,
}) => {
  await authenticate(context);

  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.locator('details.menu')).toBeVisible();
  await expect(page.locator('details.menu > summary')).toBeHidden();
  await expect(page.locator('.groups-menu')).toBeVisible();

  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto('/');
  await expect(page.locator('details.menu > summary')).toBeVisible();
  await expect(page.locator('.groups-menu')).toBeHidden();
});

// Mobile stacks the sidebar above the feed: the search/menu box comes first
// in the visual order, and entries use the full width.
test('mobile stacks sidebar above the feed', async ({ context, page }) => {
  await authenticate(context);
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto('/public');

  const sidebar = page.locator('.sidebar');
  const main = page.locator('.main');
  await expect(main.locator('[data-eid]').first()).toBeVisible();

  const sidebarBox = await sidebar.boundingBox();
  const mainBox = await main.boundingBox();
  expect(sidebarBox).not.toBeNull();
  expect(mainBox).not.toBeNull();
  expect(sidebarBox!.y).toBeLessThan(mainBox!.y);
});

test('anonymous mobile Feed starts with the SSR navigation collapsed', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto('/public');

  const navigation = page.locator('details.menu');
  await expect(navigation).toBeVisible();
  await expect(navigation).not.toHaveAttribute('open', '');
  await expect(navigation.locator('summary')).toBeVisible();
});

test('anonymous mobile Group discovery keeps its no-JS navigation collapsed', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto('/groups');

  const navigation = page.locator('details.menu');
  await expect(navigation).toBeVisible();
  await expect(navigation).not.toHaveAttribute('open', '');
  await expect(page.getByRole('heading', {name: 'Groups'})).toBeVisible();
});
