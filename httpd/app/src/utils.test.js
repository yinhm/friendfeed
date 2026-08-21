import { afterEach, describe, expect, it, vi } from 'vitest';

import { dprint, getJSON, intersperse, postForm, postJSON } from './utils';

const jsonResponse = (value) => ({ ok: true, json: vi.fn().mockResolvedValue(value) });

const errorResponse = (status, body) => ({
  ok: false,
  status,
  text: vi.fn().mockResolvedValue(typeof body === 'string' ? body : JSON.stringify(body)),
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('request helpers', () => {
  it('gets JSON with same-origin credentials and no cache', async () => {
    const response = jsonResponse({ ok: true });
    const fetchMock = vi.fn().mockResolvedValue(response);
    vi.stubGlobal('fetch', fetchMock);

    await expect(getJSON('/feed/public')).resolves.toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledWith(
      '/feed/public',
      expect.objectContaining({
        cache: 'no-cache',
        credentials: 'same-origin',
        method: 'GET',
      })
    );
    expect(response.json).toHaveBeenCalledOnce();
  });

  it('posts URL-encoded JSON commands', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ followed: true }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(
      postJSON('/a/follow', { feed_uuid: 'feed id', action: 'follow' })
    ).resolves.toEqual({ followed: true });

    const [, options] = fetchMock.mock.calls[0];
    expect(options).toMatchObject({
      credentials: 'same-origin',
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });
    expect(options.body).toBeInstanceOf(URLSearchParams);
    expect(options.body.toString()).toBe('feed_uuid=feed+id&action=follow');
  });

  it('posts FormData without overriding its multipart content type', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ id: 'entry-id' }));
    vi.stubGlobal('fetch', fetchMock);
    const formData = new FormData();
    formData.set('body', 'FriendFeed entry');

    await expect(postForm('/a/share', formData)).resolves.toEqual({
      id: 'entry-id',
    });

    const [, options] = fetchMock.mock.calls[0];
    expect(options.body).toBe(formData);
    expect(options.headers).not.toHaveProperty('Content-Type');
    expect(options).toMatchObject({
      credentials: 'same-origin',
      method: 'POST',
    });
  });

  it('surfaces the server reason from a JSON error body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      errorResponse(409, { error: 'Group admin must be demoted first' })));

    await expect(postJSON('/a/follow', { action: 'unfollow' }))
      .rejects.toThrow('Group admin must be demoted first');
  });

  it('surfaces plain-text error pages without a SyntaxError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(errorResponse(500, 'Server error.')));

    await expect(postJSON('/a/follow', { action: 'unfollow' }))
      .rejects.toThrow('Server error.');
  });

  it('falls back to the status code on an empty error body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(errorResponse(502, '')));

    await expect(postJSON('/a/follow', { action: 'unfollow' }))
      .rejects.toThrow('Request failed (502)');
  });
});

describe('small utilities', () => {
  it('prints through the browser console', () => {
    const log = vi.spyOn(window.console, 'log').mockImplementation(() => {});
    dprint('diagnostic');
    expect(log).toHaveBeenCalledWith('diagnostic');
  });

  it('intersperses values without mutating empty or populated input', () => {
    const values = [1, 2, 3];
    expect(intersperse([], '|')).toEqual([]);
    expect(intersperse(values, '|')).toEqual([1, '|', 2, '|', 3]);
    expect(values).toEqual([1, 2, 3]);
  });
});
