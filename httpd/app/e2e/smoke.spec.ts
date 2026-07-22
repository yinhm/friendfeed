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

test('public feed renders seeded entries end to end', async ({ page }) => {
  const pageErrors: string[] = [];
  const editorRequests: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  page.on('request', (request) => {
    if (/\/editor-[^/]+\.js$/.test(new URL(request.url()).pathname)) {
      editorRequests.push(request.url());
    }
  });

  await page.goto('/public');

  // [data-eid] only exists on React-rendered entries, so these assertions
  // prove the client mount happened and rendered through the rawBody path.
  // The feed is newest-first, so assert content regardless of position.
  await expect(
    page.locator('[data-eid] .content', { hasText: 'E2E smoke' })
  ).toBeVisible();
  await expect(
    page.locator('[data-eid] .content strong', { hasText: 'bold marker' })
  ).toBeVisible();
  await expect(
    page.locator('[data-eid] .content', { hasText: 'E2E second entry plain text' })
  ).toBeVisible();

  expect(pageErrors).toEqual([]);
  expect(editorRequests).toEqual([]);
});

test('authenticated user can publish from the Home editor', async ({
  context,
  page,
}) => {
  await authenticate(context);

  const editorRequests: string[] = [];
  page.on('request', (request) => {
    if (/\/editor-[^/]+\.js$/.test(new URL(request.url()).pathname)) {
      editorRequests.push(request.url());
    }
  });

  await page.goto('/');

  const editor = page.locator('[contenteditable="true"]');
  await expect(editor).toBeVisible();
  await expect.poll(() => editorRequests.length).toBeGreaterThan(0);

  const text = `E2E authenticated post ${Date.now()}`;
  await editor.fill(text);
  await page.locator('.sharebox input.submit').click();

  await expect(page.locator('[data-eid] .content', { hasText: text })).toBeVisible();
});

test('authenticated owner can edit an existing entry', async ({
  context,
  page,
}) => {
  await authenticate(context);

  await page.goto('/public');

  const originalEntry = page.locator('[data-eid]', {
    hasText: 'E2E editable original',
  });
  const entryId = await originalEntry.getAttribute('data-eid');
  if (!entryId) {
    throw new Error('editable fixture is missing data-eid');
  }
  const entry = page.locator(`[data-eid="${entryId}"]`);
  await entry.getByRole('button', { name: 'Edit' }).click();

  const editor = entry.locator('[contenteditable="true"]');
  await expect(editor).toBeVisible();
  await expect(editor).toContainText('E2E editable original');

  const editedText = `E2E edited entry ${Date.now()}`;
  await editor.fill(editedText);
  await entry.locator('input.submit').click();

  await expect(entry.locator('.content')).toContainText(editedText);
  await expect(entry.locator('[contenteditable="true"]')).toHaveCount(0);
});

test('authenticated user can comment on an entry', async ({ context, page }) => {
  await authenticate(context);

  await page.goto('/public');

  const entry = page.locator('[data-eid]', { hasText: 'E2E smoke' });
  await entry.getByRole('button', { name: 'Comment' }).click();

  const commentText = `E2E comment ${Date.now()}`;
  const comment = entry.getByRole('textbox', { name: 'Comment' });
  await comment.fill(commentText);
  await entry.getByRole('button', { name: 'Post' }).click();

  await expect(entry.locator('.comment').filter({ hasText: commentText })).toBeVisible();
  await expect(comment).toHaveCount(0);
});

test('authenticated user can like and unlike an entry', async ({
  context,
  page,
}) => {
  await authenticate(context);

  await page.goto('/public');

  const entry = page.locator('[data-eid]', {
    hasText: 'E2E second entry plain text',
  });
  await entry.getByRole('button', { name: 'Like', exact: true }).click();

  await expect(
    entry.getByRole('button', { name: 'Unlike', exact: true })
  ).toBeVisible();
  await expect(entry.locator('.likes')).toContainText('E2E User liked this');

  await entry.getByRole('button', { name: 'Unlike', exact: true }).click();

  await expect(
    entry.getByRole('button', { name: 'Like', exact: true })
  ).toBeVisible();
  await expect(entry.locator('.likes')).toHaveCount(0);
});

test('authenticated owner can confirm and delete an entry', async ({
  context,
  page,
}) => {
  await authenticate(context);

  await page.goto('/public');

  const originalEntry = page.locator('[data-eid]', {
    hasText: 'E2E deletable original',
  });
  const entryId = await originalEntry.getAttribute('data-eid');
  if (!entryId) {
    throw new Error('deletable fixture is missing data-eid');
  }
  const entry = page.locator(`[data-eid="${entryId}"]`);
  await entry.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(entry).toContainText('Confirm Delete');
  await entry.getByRole('button', { name: '取消', exact: true }).click();
  await expect(entry).not.toContainText('Confirm Delete');

  await entry.getByRole('button', { name: 'Delete', exact: true }).click();
  await entry.getByRole('button', { name: '确定', exact: true }).click();

  await expect(entry).toContainText('entry deleted.');

  await page.reload();
  await expect(page.getByText('E2E deletable original')).toHaveCount(0);
});
