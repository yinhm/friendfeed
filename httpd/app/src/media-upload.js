// @ts-check

import { postForm } from './utils';

export const MAX_PASTED_IMAGES = 20;
export const MEDIA_UPLOAD_CONCURRENCY = 2;

/** @typedef {{assetToken: string, url: string, originalUrl: string, width: number, height: number, mimeType: string, size: number}} UploadedImage */
/** @typedef {{assetToken: string, name: string, mimeType: string, size: number}} UploadedAttachment */

/** @param {File|Blob} file @param {string=} filename */
export function uploadImage(file, filename) {
  const form = new FormData();
  form.append('file', file, filename ?? (file instanceof File ? file.name : 'clipboard-image'));
  return postForm('/a/upload', form);
}

/** @param {string} sourceUrl */
export function mirrorImage(sourceUrl) {
  const form = new FormData();
  form.append('sourceUrl', sourceUrl);
  return postForm('/a/upload', form);
}

/** @param {File} file */
export function uploadAttachment(file) {
  const form = new FormData();
  form.append('file', file, file.name);
  return postForm('/a/upload_file', form);
}

/** @param {string} dataUrl */
export function dataURLToBlob(dataUrl) {
  const match = /^data:([^;,]+);base64,([a-z0-9+/=\s]+)$/i.exec(dataUrl);
  if (!match) throw new Error('Unsupported pasted image data');
  const bytes = atob(match[2].replace(/\s/g, ''));
  const out = new Uint8Array(bytes.length);
  for (let i = 0; i < bytes.length; i += 1) out[i] = bytes.charCodeAt(i);
  return new Blob([out], { type: match[1] });
}

/** @template T,R @param {T[]} items @param {number} limit @param {(item:T,index:number)=>Promise<R>} worker */
export async function mapWithConcurrency(items, limit, worker) {
  const results = new Array(items.length);
  let next = 0;
  const run = async () => {
    while (next < items.length) {
      const index = next;
      next += 1;
      results[index] = await worker(items[index], index);
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, run));
  return results;
}

/**
 * Mirrors every image in pasted HTML and returns HTML containing only
 * canonical thumbnail URLs plus metadata used to enrich Plate image nodes.
 * @param {string} html
 * @param {(blob: Blob, filename?: string) => Promise<UploadedImage>} upload
 * @param {(url: string) => Promise<UploadedImage>} mirror
 */
export async function mirrorPastedHTML(html, upload = uploadImage, mirror = mirrorImage) {
  const document = new DOMParser().parseFromString(html, 'text/html');
  const images = Array.from(document.querySelectorAll('img'));
  if (images.length > MAX_PASTED_IMAGES) {
    throw new Error(`A paste may contain at most ${MAX_PASTED_IMAGES} images`);
  }
  const metadata = await mapWithConcurrency(images, MEDIA_UPLOAD_CONCURRENCY, async (image) => {
    const source = image.getAttribute('src') ?? '';
    /** @type {UploadedImage} */
    let result;
    if (source.startsWith('data:')) {
      result = await upload(dataURLToBlob(source), 'clipboard-image');
    } else if (source.startsWith('blob:')) {
      const response = await fetch(source);
      if (!response.ok) throw new Error('Unable to read pasted image');
      result = await upload(await response.blob(), 'clipboard-image');
    } else {
      const parsed = new URL(source);
      if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
        throw new Error('Unsupported pasted image URL');
      }
      result = await mirror(parsed.toString());
    }
    image.setAttribute('src', result.url);
    return result;
  });
  return { html: document.body.innerHTML, metadata };
}

/** @param {any[]} nodes @param {UploadedImage[]} metadata */
export function enrichImageNodes(nodes, metadata) {
  let index = 0;
  const visit = (/** @type {any} */ node) => {
    if (node?.type === 'img' && metadata[index]) {
      const image = metadata[index];
      node.url = image.url;
      node.originalUrl = image.originalUrl;
      node.assetToken = image.assetToken;
      node.width = image.width;
      node.height = image.height;
      index += 1;
    }
    for (const child of node?.children ?? []) visit(child);
  };
  for (const node of nodes) visit(node);
  return nodes;
}
