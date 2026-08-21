// @ts-check
import { expect, test } from '@playwright/test';

// The sidebar search box is a React island (#search): it only renders when
// the client bundle mounts. Submitting it lands on the SSR results page.
test('sidebar search mounts and finds a seeded entry', async ({ page }) => {
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
test('search results render in the SSR document', async ({ page }) => {
  const response = await page.request.get('/search?q=smoke');
  expect(response.ok()).toBeTruthy();
  const html = await response.text();

  expect(html).toContain('E2E smoke');
  expect(html).toContain('<div class="entry" eid=');
});
