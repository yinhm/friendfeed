import { describe, expect, it } from 'vitest';

import { mediaUrlParsers } from './media-embed-element';

describe('media URL parsing', () => {
  it('rejects oversized URLs before third-party parsers run', () => {
    const oversizedUrl = `https://example.com/${'a'.repeat(2049)}`;

    for (const parse of mediaUrlParsers) {
      expect(parse(oversizedUrl)).toBeUndefined();
    }
  });
});
