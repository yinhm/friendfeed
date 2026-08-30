// @ts-check
import {expect, test, type BrowserContext, type Page} from '@playwright/test';

async function authenticate(context: BrowserContext) {
  const baseURL = process.env.E2E_BASE_URL;
  const sessionCookie = process.env.E2E_SESSION_COOKIE;
  if (!baseURL || !sessionCookie) throw new Error('E2E auth environment is required');
  await context.addCookies([{
    name: 'ffdbsess', value: sessionCookie, url: baseURL,
    httpOnly: true, sameSite: 'Lax',
  }]);
}

async function issueToken(page: Page) {
  let response = await page.request.post('/feed/e2e-user/api/generate');
  if (response.status() === 409) {
    response = await page.request.post('/feed/e2e-user/api/rotate');
  }
  expect(response.ok()).toBeTruthy();
  const payload = await response.json();
  expect(payload.token).toMatch(/^ffk1_/);
  return payload.token as string;
}

test('Public Feed API reads and creates text, image and document entries', async ({context, page}) => {
  await authenticate(context);
  const token = await issueToken(page);
  const headers = {Authorization: `Bearer ${token}`};

  const feed = await page.request.get('/api/v1/feed', {headers});
  expect(feed.status()).toBe(200);
  expect((await feed.json()).data.id).toBe('e2e-user');

  const text = await page.request.post('/api/v1/feed/entries', {
    headers,
    multipart: {title: 'API text', body_html: '<p>Created by API</p><img src="https://remote.invalid/x.jpg">'},
  });
  expect(text.status()).toBe(201);
  const textEntry = (await text.json()).data;
  expect(textEntry.body_html).toBe('<p>Created by API</p>');

  const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64');
  const media = await page.request.post('/api/v1/feed/entries', {
    headers,
    multipart: {
      title: 'API media',
      file: {name: 'pixel.png', mimeType: 'image/png', buffer: png},
    },
  });
  expect(media.status()).toBe(201);
  expect((await media.json()).data.images).toHaveLength(1);

  const document = await page.request.post('/api/v1/feed/entries', {
    headers,
    multipart: {
      title: 'API document',
      file: {name: 'report.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.7\nE2E')},
    },
  });
  expect(document.status()).toBe(201);
  expect((await document.json()).data.files).toHaveLength(1);

  const single = await page.request.get(`/api/v1/feed/entries/${textEntry.id}`, {headers});
  expect(single.status()).toBe(200);
  expect((await single.json()).data.id).toBe(textEntry.id);

  const revoke = await page.request.post('/feed/e2e-user/api/revoke');
  expect(revoke.ok()).toBeTruthy();
  const rejected = await page.request.get('/api/v1/feed', {headers});
  expect(rejected.status()).toBe(401);
});
