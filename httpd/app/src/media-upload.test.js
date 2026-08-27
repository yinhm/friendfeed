import {dataURLToBlob, enrichImageNodes, mapWithConcurrency, mirrorPastedHTML} from './media-upload';

test('converts pasted base64 images to binary blobs', async () => {
  const blob = dataURLToBlob('data:image/png;base64,aGVsbG8=');
  expect(blob.type).toBe('image/png');
  expect(await blob.text()).toBe('hello');
});

test('mirrors remote and embedded images before inserting pasted HTML', async () => {
  const upload = vi.fn().mockResolvedValue({
    assetToken: 'token-a', originalUrl: 'https://media.example/original-a', url: 'https://media.example/thumb-a',
    width: 10, height: 20, mimeType: 'image/png', size: 5,
  });
  const mirror = vi.fn().mockResolvedValue({
    assetToken: 'token-b', originalUrl: 'https://media.example/original-b', url: 'https://media.example/thumb-b',
    width: 30, height: 40, mimeType: 'image/jpeg', size: 6,
  });

  const result = await mirrorPastedHTML(
    '<p><img src="data:image/png;base64,aGVsbG8="><img src="https://remote.example/b.jpg"></p>',
    upload,
    mirror
  );

  expect(upload).toHaveBeenCalledOnce();
  expect(mirror).toHaveBeenCalledWith('https://remote.example/b.jpg');
  expect(result.html).toContain('https://media.example/thumb-a');
  expect(result.html).toContain('https://media.example/thumb-b');
  const nodes = [{type: 'p', children: [
    {type: 'img', url: 'old-a', children: [{text: ''}]},
    {type: 'img', url: 'old-b', children: [{text: ''}]},
  ]}];
  enrichImageNodes(nodes, result.metadata);
  expect(nodes[0].children[1]).toMatchObject({
    originalUrl: 'https://media.example/original-b', width: 30, height: 40,
  });
});

test('limits concurrent media operations', async () => {
  let active = 0;
  let maximum = 0;
  await mapWithConcurrency([1, 2, 3, 4, 5], 2, async value => {
    active += 1;
    maximum = Math.max(maximum, active);
    await new Promise(resolve => setTimeout(resolve, 1));
    active -= 1;
    return value;
  });
  expect(maximum).toBe(2);
});

test('skips malformed pasted images without discarding surrounding content', async () => {
  const result = await mirrorPastedHTML('<p>before<img src="relative.png">after</p>', vi.fn(), vi.fn());
  expect(result.html).toContain('before');
  expect(result.html).toContain('after');
  expect(result.html).not.toContain('<img');
  expect(result.metadata).toEqual([]);
});

test('resolves protocol-relative pasted images without failing the paste', async () => {
  const mirror = vi.fn().mockResolvedValue({assetToken: 'token', url: 'https://media/thumb', originalUrl: 'https://media/original', width: 1, height: 1, mimeType: 'image/png', size: 1});
  await mirrorPastedHTML('<img src="//cdn.example/image.png">', vi.fn(), mirror);
  expect(mirror).toHaveBeenCalledWith(expect.stringMatching(/^https?:\/\/cdn\.example\/image\.png$/));
});
