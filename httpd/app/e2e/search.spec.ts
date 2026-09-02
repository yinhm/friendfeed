// @ts-check
import { expect, test, type BrowserContext } from '@playwright/test';

async function authenticate(context: BrowserContext) {
  const baseURL = process.env.E2E_BASE_URL;
  const sessionCookie = process.env.E2E_SESSION_COOKIE;
  if (!baseURL || !sessionCookie) throw new Error('E2E auth environment is required');
  await context.addCookies([{name: 'ffdbsess', value: sessionCookie, url: baseURL,
    httpOnly: true, sameSite: 'Lax'}]);
}

// The sidebar search box is a React island (#search): it only renders when
// the client bundle mounts. Submitting it lands on the SSR results page.
test('sidebar search mounts for logged-in users and finds a seeded entry', async ({ context, page }) => {
  await authenticate(context);
  await page.goto('/public');

  const input = page.locator('#search-query');
  await expect(input).toBeVisible();

  await input.fill('second');
  await input.press('Enter');

  await expect(page).toHaveURL(/\/search\?q=second/);
  await expect(
    page.locator('[data-eid] .content', { hasText: 'E2E second entry plain text' })
  ).toBeVisible();
});

// The results page itself is server-rendered and works without the client
// bundle.
test('search results render for logged-in users', async ({ context, page }) => {
  await authenticate(context);
  const response = await page.request.get('/search?q=smoke');
  expect(response.ok()).toBeTruthy();
  const html = await response.text();

  expect(html).toContain('E2E smoke');
  expect(html).toContain('<div class="entry" eid=');
});

test('anonymous search redirects to login', async ({ page }) => {
  const response = await page.request.get('/search?q=smoke', {maxRedirects: 0});
  expect(response.status()).toBe(302);
  expect(response.headers().location).toContain('/auth/');
});
