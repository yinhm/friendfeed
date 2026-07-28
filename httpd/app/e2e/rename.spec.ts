// @ts-check
import {
  expect,
  test,
  type APIRequestContext,
  type APIResponse,
  type BrowserContext,
  type Response,
} from '@playwright/test';

async function authenticate(context: BrowserContext) {
  const baseURL = process.env.E2E_BASE_URL;
  const sessionCookie = process.env.E2E_RENAME_SESSION_COOKIE;
  if (!baseURL || !sessionCookie) {
    throw new Error('E2E_BASE_URL and E2E_RENAME_SESSION_COOKIE are required');
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

async function requireOk<T extends APIResponse | Response>(
  response: T,
  operation: string
): Promise<T> {
  if (!response.ok()) {
    throw new Error(
      `${operation}: status ${response.status()}: ${await response.text()}`
    );
  }
  return response;
}

async function postCleanup(
  request: APIRequestContext,
  path: string,
  form: Record<string, string>,
  operation: string
) {
  return requireOk(await request.post(path, { form }), operation);
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

  let entryId: string | null = null;
  let botEntryId: string | null = null;
  let commentId: string | null = null;
  let liked = false;
  let testError: unknown = null;
  const cleanupErrors: string[] = [];

  try {
    // Post an entry as the session user. Capture the authoritative id
    // from the response before making any UI assertion.
    await page.goto('/');
    const editor = page.locator('[contenteditable="true"]');
    await expect(editor).toBeVisible();
    await editor.fill(text);
    const entryResponsePromise = page.waitForResponse(
      (resp) =>
        new URL(resp.url()).pathname === '/a/share' &&
        resp.request().method() === 'POST'
    );
    await page.locator('.sharebox button.submit').click();
    const entryResponse = await requireOk(
      await entryResponsePromise,
      'create entry'
    );
    const createdEntry = (await entryResponse.json()) as { id?: string };
    if (!createdEntry.id) {
      throw new Error('create entry: response has no id');
    }
    entryId = createdEntry.id;
    await expect(page.locator('[data-eid] .content', { hasText: text })).toBeVisible();

    // Comment on a bot entry; capture both ids at creation time.
    await page.goto('/public');
    const botEntry = page.locator('[data-eid]', {
      hasText: 'E2E second entry plain text',
    });
    botEntryId = await botEntry.getAttribute('data-eid');
    if (!botEntryId) {
      throw new Error('bot entry is missing data-eid');
    }

    await botEntry.getByRole('button', { name: 'Comment' }).click();
    const commentResponse = page.waitForResponse(
      (resp) =>
        new URL(resp.url()).pathname === '/a/comment' &&
        resp.request().method() === 'POST'
    );
    await botEntry.getByRole('textbox', { name: 'Comment' }).fill(commentText);
    await botEntry.getByRole('button', { name: 'Post' }).click();
    const createdCommentResponse = await requireOk(
      await commentResponse,
      'create comment'
    );
    const createdComment = (await createdCommentResponse.json()) as { id?: string };
    if (!createdComment.id) {
      throw new Error('create comment: response has no id');
    }
    commentId = createdComment.id;
    await expect(botEntry.locator('.comment', { hasText: commentText })).toBeVisible();

    const likeResponse = page.waitForResponse(
      (resp) =>
        new URL(resp.url()).pathname === '/a/like' &&
        resp.request().method() === 'POST'
    );
    await botEntry.getByRole('button', { name: 'Like', exact: true }).click();
    await requireOk(await likeResponse, 'like entry');
    // Set this before UI assertions: the server mutation has completed.
    liked = true;
    await expect(botEntry.getByRole('button', { name: 'Unlike', exact: true })).toBeVisible();

    // Rename both the profile id (feed slug) and the display name.
    await page.goto('/account/profile');
    await page.locator('#id').fill(newId);
    await page.locator('#name').fill('E2E Renamed');
    await page.getByRole('button', { name: 'Save Changes' }).click();
    await expect(page.getByRole('status')).toContainText(`/feed/${newId}`);

    // The old feed URL follows the soft-rename record to the canonical ID.
    await page.goto('/feed/e2e-rename-user');
    await expect(page).toHaveURL(new RegExp(`/feed/${newId}$`));

    // Read the entry through its captured permalink rather than the public
    // cache, whose background rebuild may legitimately replace recent
    // in-memory pushes. Its author follows the canonical profile.
    await page.goto(`/e/${entryId}`);
    const ownEntry = page.locator('[data-eid]', { hasText: text });
    const authorLink = ownEntry.locator('.author a').first();
    await expect(authorLink).toHaveText('E2E Renamed');
    await expect(authorLink).toHaveAttribute('href', `/feed/${newId}`);

    await page.goto('/public');

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
    const unlikeResponse = page.waitForResponse(
      (resp) =>
        new URL(resp.url()).pathname === '/a/like/delete' &&
        resp.request().method() === 'POST'
    );
    await renamedBotEntry.getByRole('button', { name: 'Unlike', exact: true }).click();
    await requireOk(await unlikeResponse, 'unlike entry');
    // Clear this before UI assertions: the server mutation has completed.
    liked = false;
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
  } catch (e) {
    testError = e;
  } finally {
    // Best-effort cleanup via captured ids, independent of the current
    // page. Each step is isolated so one failure cannot skip the rest.
    if (liked && botEntryId) {
      try {
        await postCleanup(
          page.request,
          '/a/like/delete',
          { entry: botEntryId },
          'cleanup unlike'
        );
      } catch (e) {
        cleanupErrors.push(`unlike: ${e}`);
      }
    }
    if (commentId && botEntryId) {
      try {
        await postCleanup(
          page.request,
          '/a/comment/delete',
          { entry: botEntryId, comment: commentId },
          'cleanup comment'
        );
      } catch (e) {
        cleanupErrors.push(`delete comment: ${e}`);
      }
    }
    if (entryId) {
      try {
        await postCleanup(
          page.request,
          '/a/delete',
          { entry: entryId },
          'cleanup entry'
        );
      } catch (e) {
        cleanupErrors.push(`delete entry: ${e}`);
      }
    }
  }

  // Preserve the body failure while also reporting cleanup failures:
  // either condition must leave an explicit red result.
  if (testError !== null) {
    if (cleanupErrors.length > 0) {
      throw new Error(
        `test failed: ${String(testError)}; cleanup also failed: ${cleanupErrors.join('; ')}`,
        { cause: testError }
      );
    }
    throw testError;
  }
  if (cleanupErrors.length > 0) {
    throw new Error(`cleanup failed: ${cleanupErrors.join('; ')}`);
  }
});
