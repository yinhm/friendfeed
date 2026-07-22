// @ts-check
import { expect, test } from '@playwright/test';

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
