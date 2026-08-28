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

test('uploaded image is promoted when its entry is published', async ({ context, page }) => {
  await authenticate(context);
  await page.goto('/');

  const marker = `E2E image upload ${Date.now()}`;
  const editor = page.locator('[contenteditable="true"]');
  await editor.fill(marker);
  await page.locator('label[aria-label="Add image"] input[type=file]').setInputFiles({
    name: 'pixel.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=',
      'base64',
    ),
  });

  // Plate may keep a 1x1 fixture visually collapsed, but the staged node must
  // exist before publish so promotion is exercised rather than raced.
  await expect(page.locator('.sharebox img')).toHaveAttribute('src', /upload-staging/);
  await page.locator('.sharebox button.submit').click();

  const entry = page.locator('[data-eid]', { hasText: marker });
  const image = entry.locator('img').first();
  await expect(image).toBeVisible();
  await expect(image).not.toHaveAttribute('src', /upload-staging/);
});
