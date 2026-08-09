import { describe, expect, test } from 'vitest';

import { parseVideoUrl } from './video-url-parser';

describe('parseVideoUrl', () => {
  test.each([
    ['https://youtu.be/dQw4w9WgXcQ'],
    ['https://www.youtube.com/watch?v=dQw4w9WgXcQ'],
    ['https://www.youtube.com/shorts/dQw4w9WgXcQ'],
  ])('parses YouTube URL: %s', (input) => {
    expect(parseVideoUrl(input)).toEqual({
      provider: 'youtube',
      id: 'dQw4w9WgXcQ',
      url: 'https://www.youtube.com/embed/dQw4w9WgXcQ',
    });
  });

  test.each([
    'not a URL',
    'javascript:alert(1)',
    'https://vimeo.com/76979871',
    'https://www.dailymotion.com/video/x8abc12',
    'https://v.youku.com/v_show/id_XNDQ0.html',
    'https://coub.com/view/abc123',
    'https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ',
    'https://vimeo.com.evil.example/76979871',
    `https://youtube.com/watch?v=dQw4w9WgXcQ&padding=${'x'.repeat(2048)}`,
  ])('rejects unsupported or unsafe input: %s', (input) => {
    expect(parseVideoUrl(input)).toBeUndefined();
  });
});
