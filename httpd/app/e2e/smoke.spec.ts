// @ts-check
import { expect, test } from '@playwright/test';

test('public feed renders seeded entries end to end', async ({ page }) => {
  const pageErrors = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));

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
});
