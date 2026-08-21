// @ts-check
import { expect, test } from '@playwright/test';

// SSR first paint: the server-rendered HTML must already contain the entries
// (class="entry", eid attribute) without any client-side mount. The React
// hydration marker data-eid must NOT appear in the raw document.
test('public feed renders entries in the SSR document', async ({ page }) => {
  const response = await page.request.get('/public');
  expect(response.ok()).toBeTruthy();
  const html = await response.text();

  expect(html).toContain('E2E smoke');
  expect(html).toContain('E2E second entry plain text');
  expect(html).toContain('<div class="entry" eid=');
  expect(html).toContain('class="permalink"');
  expect(html).not.toContain('data-eid');
});

// Cursor pagination: the seed holds 39 entries on the public timeline
// (PageSize 30), so page 1 exposes a Next link and page 2 holds only older
// filler entries. The assertions stay agnostic to which fillers land on
// which page: ordering is the timeline's business, not this spec's.
test('public feed paginates with a cursor', async ({ page }) => {
  await page.goto('/public');

  await expect(page.locator('[data-eid] .content', { hasText: 'E2E smoke' }).first()).toBeVisible();
  await expect(page.locator('[data-eid] .content', { hasText: 'E2E page filler' }).first()).toBeVisible();

  const next = page.locator('.pager a', { hasText: 'Next' });
  await expect(next).toBeVisible();
  await next.click();

  await expect(page).toHaveURL(/\?cursor=/);
  await expect(page.locator('[data-eid] .content', { hasText: 'E2E page filler' }).first()).toBeVisible();
  await expect(page.locator('[data-eid] .content', { hasText: 'E2E smoke' })).toHaveCount(0);
});

// Anonymous visitors never get the editor: the Home share box and the editor
// chunk stay out of the page entirely.
test('anonymous public page does not mount the editor', async ({ page }) => {
  const editorRequests: string[] = [];
  page.on('request', (request) => {
    if (/\/editor-[^/]+\.js$/.test(new URL(request.url()).pathname)) {
      editorRequests.push(request.url());
    }
  });

  await page.goto('/public');

  await expect(page.locator('[data-eid]').first()).toBeVisible();
  await expect(page.locator('.sharebox')).toHaveCount(0);
  expect(editorRequests).toEqual([]);
});
