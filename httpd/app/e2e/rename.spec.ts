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
//
// The spec shares the seeded database with the other specs, so every
// mutation is contained in try/finally: the created entry and comment
// are deleted afterwards and the profile identity is restored (the
// restore is idempotent — a no-op if the rename never happened).
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

  const newId = `e2e-u${Date.now().toString(36)}`;
  try {
    // Rename both the profile id (feed slug) and the display name.
    await page.goto('/account/profile');
    await page.locator('#id').fill(newId);
    await page.locator('#name').fill('E2E Renamed');
    await page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(page.getByRole('status')).toContainText(`/feed/${newId}`);

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
    // Best-effort cleanup over the JSON API (robust against UI timing),
    // each step independent so one failure cannot skip the rest: delete
    // the created comment and entry, then restore the seeded identity
    // (a no-op when the rename never happened).
    try {
      // /a/entry/:uuid 404s for the bot entry (its author profile does
      // not exist), so read the comment id from window.appData instead.
      const ids = await page.evaluate((body) => {
        const w = window as unknown as {
          appData: {
            feed: {
              entries: { id: string; comments?: { id: string; body?: string }[] }[];
            };
          };
        };
        for (const e of w.appData.feed.entries) {
          for (const c of e.comments ?? []) {
            if (c.body?.includes(body)) {
              return { entry: e.id, comment: c.id };
            }
          }
        }
        return null;
      }, commentText);
      if (ids) {
        await page.request.post('/a/comment/delete', { form: ids });
      }
    } catch (e) {
      console.warn('cleanup: delete comment failed', e);
    }

    try {
      const entryId = await page
        .locator('[data-eid]', { hasText: text })
        .getAttribute('data-eid');
      if (entryId) {
        await page.request.post('/a/delete', { form: { entry: entryId } });
      }
    } catch (e) {
      console.warn('cleanup: delete entry failed', e);
    }

    try {
      await page.goto('/account/profile');
      await page.locator('#id').fill('e2e-user');
      await page.locator('#name').fill('E2E User');
      await page.getByRole('button', { name: 'Save Changes' }).click();
      await expect(page.getByRole('status')).toContainText('/feed/e2e-user');
    } catch (e) {
      console.warn('cleanup: restore profile failed', e);
    }
  }
});
