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

// Plate mark hotkey: toggling Ctrl+B at the collapsed cursor turns on the
// pending bold mark, so everything typed after it must publish as <strong>.
// (Selecting existing text first is unreliable in headless Chromium: neither
// Control+A nor selectText() reaches the Slate selection there.)
//
// The hotkey races editor setup on slow runners: Ctrl+B fired before Slate
// has registered a selection is silently dropped while typing still lands,
// so verify the mark and retry from a clean editor instead of asserting
// blindly. A dropped toggle leaves the mark state unchanged, so retrying
// the toggle is safe.
test('editor publishes bold text via keyboard shortcut', async ({
  context,
  page,
}) => {
  await authenticate(context);
  await page.goto('/');

  const editor = page.locator('[contenteditable="true"]');
  await expect(editor).toBeVisible();

  const text = `E2E bold post ${Date.now()}`;
  await editor.click();

  const strong = editor.locator('strong');
  let bolded = false;
  for (let attempt = 0; attempt < 4 && !bolded; attempt++) {
    await editor.press('Control+B');
    await editor.pressSequentially(text);
    try {
      // The mark must exist in the editor before it can survive serialization.
      await expect(strong).toHaveText(text, { timeout: 2000 });
      bolded = true;
    } catch {
      // Hotkey dropped: remove exactly what was typed, then retry the toggle.
      for (let i = 0; i < text.length; i++) {
        await editor.press('Backspace');
      }
    }
  }
  expect(bolded, 'Ctrl+B never produced a bold mark').toBe(true);

  await page.locator('.sharebox button.submit').click();

  const entry = page.locator('[data-eid]', { hasText: text });
  await expect(entry.locator('.content strong')).toHaveText(text);
});

// Shift+Enter is the soft-break contract: the two lines stay in a single
// paragraph (Enter would split it, and nothing submits early).
test('editor Shift+Enter inserts a soft break', async ({ context, page }) => {
  await authenticate(context);
  await page.goto('/');

  const editor = page.locator('[contenteditable="true"]');
  await expect(editor).toBeVisible();

  const marker = `${Date.now()}`;
  const line1 = `E2E softbreak ${marker} first line`;
  const line2 = `second line ${marker}`;
  await editor.click();
  await editor.pressSequentially(line1);
  await editor.press('Shift+Enter');
  await editor.pressSequentially(line2);
  await page.locator('.sharebox button.submit').click();

  const entry = page.locator('[data-eid]', { hasText: marker });
  const paragraphs = entry.locator('.content p');
  await expect(paragraphs).toHaveCount(1);
  await expect(paragraphs.first()).toContainText(line1);
  await expect(paragraphs.first()).toContainText(line2);
});

// The `> ` markdown rule wraps the block in a blockquote, and the rawBody
// round-trip through Edit must preserve it (AGENTS.md: blockquote works in
// both the old flat and the new blockquote > p shapes).
test('blockquote survives publish and edit round-trip', async ({
  context,
  page,
}) => {
  await authenticate(context);
  await page.goto('/');

  const editor = page.locator('[contenteditable="true"]');
  await expect(editor).toBeVisible();

  const text = `E2E quote ${Date.now()} survives editing`;
  await editor.click();
  await editor.pressSequentially(`> ${text}`);
  await expect(editor.locator('blockquote')).toContainText(text);
  await page.locator('.sharebox button.submit').click();

  const entry = page.locator('[data-eid]', { hasText: text });
  await expect(entry.locator('.content blockquote')).toContainText(text);

  await entry.getByRole('button', { name: 'Edit' }).click();
  const editBox = entry.locator('[contenteditable="true"]');
  await expect(editBox).toBeVisible();
  await expect(editBox.locator('blockquote')).toContainText(text);
  await entry.locator('button.submit').click();

  await expect(entry.locator('.content blockquote')).toContainText(text);
  await expect(entry.locator('[contenteditable="true"]')).toHaveCount(0);
});
