// @ts-check
import {expect, test, type BrowserContext, type Page} from '@playwright/test';

async function authenticate(context: BrowserContext) {
  const baseURL = process.env.E2E_BASE_URL;
  const sessionCookie = process.env.E2E_SESSION_COOKIE;
  if (!baseURL || !sessionCookie) {
    throw new Error('E2E_BASE_URL and E2E_SESSION_COOKIE are required');
  }
  await context.addCookies([{
    name: 'ffdbsess', value: sessionCookie, url: baseURL,
    httpOnly: true, sameSite: 'Lax',
  }]);
}

async function confirm(page: Page, action: 'Generate' | 'Revoke') {
  await page.getByRole('button', {name: action, exact: true}).first().click();
  await page.locator('[popover]:popover-open').getByRole('button', {name: action, exact: true}).click();
}

test('Feed API key is displayed once and can be revoked', async ({context, page}) => {
  await authenticate(context);
  await page.goto('/feed/e2e-user/api');

  if (await page.getByText('Active', {exact: true}).count()) {
    await confirm(page, 'Revoke');
    await expect(page.getByText('Revoked', {exact: true})).toBeVisible();
  }

  const fresh = page.getByRole('button', {name: 'Generate new key'});
  if (await fresh.count()) {
    await fresh.click();
  } else {
    await confirm(page, 'Generate');
  }
  const token = page.locator('[role="status"] code');
  await expect(token).toContainText('ffk1_');

  await page.reload();
  await expect(page.locator('[role="status"] code')).toHaveCount(0);
  await expect(page.getByText('Active', {exact: true})).toBeVisible();

  await confirm(page, 'Revoke');
  await expect(page.getByText('Revoked', {exact: true})).toBeVisible();
});
