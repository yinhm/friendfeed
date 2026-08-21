// @ts-check
import {
  expect,
  test,
  type APIRequestContext,
  type BrowserContext,
} from '@playwright/test';

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

// SSR group lifecycle: create through the plain form, land on the group feed,
// see the group in the sidebar and the groups index, get the server's error
// banner on a conflicting ID, then delete the group again.
test('group create, sidebar navigation, duplicate rejection and delete', async ({
  context,
  page,
}) => {
  await authenticate(context);

  const suffix = Date.now().toString(36);
  const groupId = `e2eg${suffix}`;
  const groupName = `E2E Group ${suffix}`;

  let created = false;
  let testError: unknown = null;
  let cleanupError: unknown = null;

  try {
    await page.goto('/groups/create');
    await page.locator('#group-id').fill(groupId);
    await page.locator('#group-name').fill(groupName);
    await page.locator('#group-description').fill('created by the e2e suite');
    await page.getByRole('button', { name: 'Create Group' }).click();

    await expect(page).toHaveURL(new RegExp(`/feed/${groupId}$`));
    created = true;
    await expect(page.locator('.header h1 a')).toHaveText(groupName);

    // The sidebar Groups block links the new group on every page.
    await page.goto('/');
    await expect(
      page.locator('.groups-menu a', { hasText: groupName })
    ).toHaveAttribute('href', `/feed/${groupId}`);

    // The groups index lists it too.
    await page.goto('/groups');
    await expect(
      page.locator('.main a', { hasText: groupName })
    ).toBeVisible();

    // A conflicting ID redisplays the form with the server's message.
    await page.goto('/groups/create');
    await page.locator('#group-id').fill(groupId);
    await page.locator('#group-name').fill(`${groupName} duplicate`);
    await page.getByRole('button', { name: 'Create Group' }).click();
    await expect(page).toHaveURL(/\/groups\/create$/);
    await expect(page.locator('.error-banner')).toBeVisible();
    await expect(page.locator('#group-name')).toHaveValue(`${groupName} duplicate`);
  } catch (e) {
    testError = e;
  } finally {
    if (created) {
      try {
        const response = await deleteGroup(page.request, groupId);
        if (!response.ok()) {
          throw new Error(
            `delete group: status ${response.status()}: ${await response.text()}`
          );
        }
      } catch (e) {
        cleanupError = e;
      }
    }
  }

  if (testError !== null) {
    throw testError;
  }
  if (cleanupError !== null) {
    throw new Error(`cleanup failed: ${cleanupError}`);
  }

  // Navigate fresh: the deletion must remove the sidebar link.
  await page.goto('/');
  await expect(
    page.locator('.groups-menu a', { hasText: groupName })
  ).toHaveCount(0);
});

async function deleteGroup(request: APIRequestContext, groupId: string) {
  return request.post(`/groups/${groupId}/delete`);
}
