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

// Client-side validation mirrors model.ValidateProfileId: an invalid ID shows
// the inline error and blocks submission — no save request, no status banner.
test('profile form rejects an invalid ID without saving', async ({
  context,
  page,
}) => {
  await authenticate(context);
  await page.goto('/account/profile');

  const saveRequests: string[] = [];
  page.on('request', (request) => {
    if (
      new URL(request.url()).pathname === '/account/profile' &&
      request.method() === 'POST'
    ) {
      saveRequests.push(request.url());
    }
  });

  await page.locator('#id').fill('ab');
  await expect(page.getByText('At least 4 characters')).toBeVisible();

  await page.getByRole('button', { name: 'Save Changes' }).click();
  await expect(page.getByRole('alert')).toContainText('At least 4 characters');
  await expect(page.getByRole('status')).toHaveCount(0);
  expect(saveRequests).toEqual([]);
});

// Tab switches are client-side and keep the URL and history in sync: Back
// from a pushed tab must land on the previous panel (the remount contract in
// account.jsx).
test('account tabs sync URL and history', async ({ context, page }) => {
  await authenticate(context);
  await page.goto('/account/profile');

  await expect(
    page.getByRole('heading', { name: 'Edit Profile' })
  ).toBeVisible();

  await page.getByRole('link', { name: 'Import Services' }).click();
  await expect(page).toHaveURL(/\/account\/import$/);
  await expect(
    page.getByRole('heading', { name: 'Import Services' })
  ).toBeVisible();
  await expect(page.getByText('No services connected yet.')).toBeVisible();

  await page.goBack();
  await expect(page).toHaveURL(/\/account\/profile$/);
  await expect(
    page.getByRole('heading', { name: 'Edit Profile' })
  ).toBeVisible();
});
