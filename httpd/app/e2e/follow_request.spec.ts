// @ts-check
import { expect, test, type Browser, type BrowserContext } from '@playwright/test';

async function authenticate(context: BrowserContext, cookieValue: string | undefined) {
  const baseURL = process.env.E2E_BASE_URL;
  if (!baseURL || !cookieValue) {
    throw new Error('E2E_BASE_URL and session cookie env vars are required');
  }
  await context.addCookies([
    {
      name: 'ffdbsess',
      value: cookieValue,
      url: baseURL,
      httpOnly: true,
      sameSite: 'Lax',
    },
  ]);
}

async function userPage(browser: Browser, cookieEnv: string | undefined) {
  const context = await browser.newContext();
  await authenticate(context, cookieEnv);
  return { context, page: await context.newPage() };
}

// Merged private-follow flow for a user feed: owner sets Private, an
// outsider hits the SSR request page, files a request, the owner approves
// on /account/requests, and the outsider can then read the feed.
test('private user feed: request, approve, read', async ({ browser }) => {
  const owner = await userPage(browser, process.env.E2E_SESSION_COOKIE);
  const outsider = await userPage(browser, process.env.E2E_RENAME_SESSION_COOKIE);

  try {
    await owner.page.goto('/account/profile');
    await owner.page.getByRole('checkbox', { name: /Private/ }).check();
    await owner.page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(owner.page.getByRole('status')).toBeVisible();

    await outsider.page.goto('/feed/e2e-user');
    await expect(outsider.page.getByText(/This feed is private/)).toBeVisible();
    await outsider.page.getByRole('button', { name: 'Request to follow' }).click();
    await expect(outsider.page.getByText(/pending approval/)).toBeVisible();

    await owner.page.goto('/account/requests');
    await expect(owner.page.getByText('E2E Rename User')).toBeVisible();
    await owner.page.getByRole('button', { name: 'Approve' }).click();
    await expect(owner.page.getByText('No pending requests.')).toBeVisible();

    await outsider.page.goto('/feed/e2e-user');
    await expect(outsider.page.locator('.header h1 a')).toHaveText('E2E User');
  } finally {
    // Restore the shared fixture: public feed, no leftover approval state.
    await owner.page.goto('/account/profile');
    await owner.page.getByRole('checkbox', { name: /Private/ }).uncheck();
    await owner.page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(owner.page.getByRole('status')).toBeVisible();
    await owner.context.close();
    await outsider.context.close();
  }
});

// The same flow for a private Group: the admin approves from the members
// page instead. The Group is deleted afterwards.
test('private group: request, admin approves, member reads', async ({ browser }) => {
  const admin = await userPage(browser, process.env.E2E_SESSION_COOKIE);
  const outsider = await userPage(browser, process.env.E2E_RENAME_SESSION_COOKIE);

  const suffix = Date.now().toString(36);
  const groupId = `e2epg${suffix}`;
  const groupName = `E2E Private Group ${suffix}`;
  let created = false;
  let testError: unknown = null;

  try {
    await admin.page.goto('/groups/create');
    await admin.page.locator('#group-id').fill(groupId);
    await admin.page.locator('#group-name').fill(groupName);
    await admin.page.getByRole('checkbox', { name: 'Private group' }).check();
    await admin.page.getByRole('button', { name: 'Create Group' }).click();
    await expect(admin.page).toHaveURL(new RegExp(`/feed/${groupId}$`));
    created = true;

    await outsider.page.goto(`/feed/${groupId}`);
    await expect(outsider.page.getByText(/This feed is private/)).toBeVisible();
    await outsider.page.getByRole('button', { name: 'Request to follow' }).click();
    await expect(outsider.page.getByText(/pending approval/)).toBeVisible();

    await admin.page.goto(`/groups/${groupId}/members`);
    await expect(admin.page.getByText('Pending requests')).toBeVisible();
    await admin.page.getByRole('button', { name: 'Approve' }).click();
    await expect(admin.page.getByText('Pending requests')).toHaveCount(0);

    await outsider.page.goto(`/feed/${groupId}`);
    await expect(outsider.page.locator('.header h1 a')).toHaveText(groupName);
  } catch (e) {
    testError = e;
  } finally {
    if (created) {
      const response = await admin.page.request.post(`/groups/${groupId}/delete`);
      if (!response.ok()) {
        testError = testError ?? new Error(`delete group: status ${response.status()}`);
      }
    }
    await admin.context.close();
    await outsider.context.close();
  }
  if (testError !== null) {
    throw testError;
  }
});
