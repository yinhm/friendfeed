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
// The spec shares the seeded database with the other specs. try starts
// before the first mutation, every created object id is captured at
// creation time, and finally cleans up via those ids alone — never via
// whatever page happens to be open after a failure. Cleanup failures
// fail the test when the body itself did not already fail.
test('profile rename propagates to author display, like state and comment commands', async ({
  context,
  page,
}) => {
  await authenticate(context);

  const text = `E2E rename post ${Date.now()}`;
  const commentText = `E2E rename comment ${Date.now()}`;
  const newId = `e2e-u${Date.now().toString(36)}`;

  let entryId = /** @type {string | null} */ (null);
  let botEntryId = /** @type {string | null} */ (null);
  let commentId = /** @type {string | null} */ (null);
  let liked = false;
  let testError = /** @type {unknown} */ (null);
  const cleanupErrors = /** @type {string[]} */ ([]);

  try {
    // Post an entry as the session user; capture its id right away.
    await page.goto('/');
    const editor = page.locator('[contenteditable="true"]');
    await expect(editor).toBeVisible();
    await editor.fill(text);
    await page.locator('.sharebox button.submit').click();
    await expect(page.locator('[data-eid] .content', { hasText: text })).toBeVisible();
    entryId = await page.locator('[data-eid]', { hasText: text }).getAttribute('data-eid');

    // Comment on a bot entry; capture both ids at creation time.
    await page.goto('/public');
    const botEntry = page.locator('[data-eid]', {
      hasText: 'E2E second entry plain text',
    });
    botEntryId = await botEntry.getAttribute('data-eid');

    await botEntry.getByRole('button', { name: 'Comment' }).click();
    const commentResponse = page.waitForResponse(
      (resp) => resp.url().includes('/a/comment') && resp.request().method() === 'POST'
    );
    await botEntry.getByRole('textbox', { name: 'Comment' }).fill(commentText);
    await botEntry.getByRole('button', { name: 'Post' }).click();
    commentId = (await (await commentResponse).json()).id;
    await expect(botEntry.locator('.comment', { hasText: commentText })).toBeVisible();

    await botEntry.getByRole('button', { name: 'Like', exact: true }).click();
    await expect(botEntry.getByRole('button', { name: 'Unlike', exact: true })).toBeVisible();
    liked = true;

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
    liked = false;

    // Comment edit works after the rename.
    const editedText = `${commentText} edited`;
    await comment.hover();
    await comment.getByRole('button', { name: 'Edit' }).click();
    await renamedBotEntry.getByRole('textbox', { name: 'Edit comment' }).fill(editedText);
    await renamedBotEntry.getByRole('button', { name: 'Post' }).click();
    await expect(renamedBotEntry.locator('.comment', { hasText: editedText })).toBeVisible();
  } catch (e) {
    testError = e;
  } finally {
    // Best-effort cleanup via captured ids, independent of the current
    // page. Each step is isolated so one failure cannot skip the rest.
    if (liked && botEntryId) {
      try {
        await page.request.post('/a/like/delete', { form: { entry: botEntryId } });
      } catch (e) {
        cleanupErrors.push(`unlike: ${e}`);
      }
    }
    if (commentId && botEntryId) {
      try {
        await page.request.post('/a/comment/delete', {
          form: { entry: botEntryId, comment: commentId },
        });
      } catch (e) {
        cleanupErrors.push(`delete comment: ${e}`);
      }
    }
    if (entryId) {
      try {
        await page.request.post('/a/delete', { form: { entry: entryId } });
      } catch (e) {
        cleanupErrors.push(`delete entry: ${e}`);
      }
    }
    try {
      // Idempotent: a no-op when the rename never happened.
      const resp = await page.request.post('/account/profile', {
        form: { id: 'e2e-user', name: 'E2E User', description: '', picture: '' },
      });
      if (!resp.ok()) {
        cleanupErrors.push(`restore profile: status ${resp.status()}: ${await resp.text()}`);
      }
    } catch (e) {
      cleanupErrors.push(`restore profile: ${e}`);
    }
  }

  // The body error wins; cleanup failures only fail an otherwise
  // passing test, so polluted shared state can never go unnoticed.
  if (testError !== null) {
    throw testError;
  }
  if (cleanupErrors.length > 0) {
    throw new Error(`cleanup failed: ${cleanupErrors.join('; ')}`);
  }
});
