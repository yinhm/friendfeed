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

// TODO.md acceptance: after a profile rename, UUID-bearing entries,
// comments and likes must keep working — display follows the canonical
// profile, like state survives, and the author keeps edit/unlike.
// The test restores the original identity in `finally` so spec files
// stay order-independent on the shared seeded database.
test('profile rename propagates to author display, like state and comment commands', async ({
  context,
  page,
}) => {
  await authenticate(context);

  // Post an entry as the session user.
  await page.goto('/');
  const editor = page.locator('[contenteditable="true"]');
  await expect(editor).toBeVisible();
  const text = `E2E rename post ${Date.now()}`;
  await editor.fill(text);
  await page.locator('.sharebox button.submit').click();
  await expect(page.locator('[data-eid] .content', { hasText: text })).toBeVisible();

  // Comment on and like a bot entry.
  await page.goto('/public');
  const botEntry = page.locator('[data-eid]', {
    hasText: 'E2E second entry plain text',
  });
  await botEntry.getByRole('button', { name: 'Comment' }).click();
  const commentText = `E2E rename comment ${Date.now()}`;
  await botEntry.getByRole('textbox', { name: 'Comment' }).fill(commentText);
  await botEntry.getByRole('button', { name: 'Post' }).click();
  await expect(botEntry.locator('.comment', { hasText: commentText })).toBeVisible();

  await botEntry.getByRole('button', { name: 'Like', exact: true }).click();
  await expect(botEntry.getByRole('button', { name: 'Unlike', exact: true })).toBeVisible();

  // Rename both the profile id (feed slug) and the display name.
  const newId = `e2e-u${Date.now().toString(36)}`;
  await page.goto('/account/profile');
  await page.locator('#id').fill(newId);
  await page.locator('#name').fill('E2E Renamed');
  await page.getByRole('button', { name: 'Save Changes' }).click();
  await expect(page.getByRole('status')).toContainText(`/feed/${newId}`);

  try {
    await page.goto('/public');

    // Own entry author shows the new identity and feed link.
    const ownEntry = page.locator('[data-eid]', { hasText: text });
    const authorLink = ownEntry.locator('.author a').first();
    await expect(authorLink).toHaveText('E2E Renamed');
    await expect(authorLink).toHaveAttribute('href', `/feed/${newId}`);

    // The comment shows the new name and keeps its Edit command.
    const renamedBotEntry = page.locator('[data-eid]', {
      hasText: 'E2E second entry plain text',
    });
    const comment = renamedBotEntry.locator('.comment', { hasText: commentText });
    await expect(comment.locator('a', { hasText: 'E2E Renamed' })).toBeVisible();
    // Own-comment commands are hover-revealed by CSS (visibility: hidden
    // until hover/focus-within), so hover before asserting them.
    await comment.hover();
    await expect(comment.getByRole('button', { name: 'Edit' })).toBeVisible();

    // Like state survived the rename: still Unlike, new name in the list.
    await expect(
      renamedBotEntry.getByRole('button', { name: 'Unlike', exact: true })
    ).toBeVisible();
    await expect(renamedBotEntry.locator('.likes')).toContainText('E2E Renamed liked this');

    // Unlike works after the rename.
    await renamedBotEntry.getByRole('button', { name: 'Unlike', exact: true }).click();
    await expect(
      renamedBotEntry.getByRole('button', { name: 'Like', exact: true })
    ).toBeVisible();

    // Comment edit works after the rename.
    const editedText = `${commentText} edited`;
    await comment.hover();
    await comment.getByRole('button', { name: 'Edit' }).click();
    await renamedBotEntry.getByRole('textbox', { name: 'Edit comment' }).fill(editedText);
    await renamedBotEntry.getByRole('button', { name: 'Post' }).click();
    await expect(renamedBotEntry.locator('.comment', { hasText: editedText })).toBeVisible();
  } finally {
    // Restore the seeded identity for the other specs.
    await page.goto('/account/profile');
    await page.locator('#id').fill('e2e-user');
    await page.locator('#name').fill('E2E User');
    await page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(page.getByRole('status')).toContainText('/feed/e2e-user');
  }
});
