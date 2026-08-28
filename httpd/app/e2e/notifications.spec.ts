// @ts-check
import { expect, test, type BrowserContext } from '@playwright/test';

async function authenticate(context: BrowserContext) {
  const baseURL = process.env.E2E_BASE_URL;
  const sessionCookie = process.env.E2E_SESSION_COOKIE;
  if (!baseURL || !sessionCookie) throw new Error('E2E authentication is required');
  await context.addCookies([{
    name: 'ffdbsess', value: sessionCookie, url: baseURL,
    httpOnly: true, sameSite: 'Lax',
  }]);
}

test('authenticated user can read a notification', async ({ context, page }) => {
  await authenticate(context);
  await page.goto('/notifications');

  const notification = page.getByText(/E2E Bot liked your post/i);
  await expect(notification).toBeVisible();

  // Rendering completes before the best-effort mark-read mutation. A reload
  // must remain a valid, readable page even though the notification is read.
  await page.reload();
  await expect(notification).toBeVisible();
});
