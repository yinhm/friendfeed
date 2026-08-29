import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
import LiteYouTubeEmbed from 'react-lite-youtube-embed';

import { mediaUrlParsers } from './media-embed-element';

describe('media URL parsing', () => {
  it('rejects oversized URLs before third-party parsers run', () => {
    const oversizedUrl = `https://example.com/${'a'.repeat(2049)}`;

    for (const parse of mediaUrlParsers) {
      expect(parse(oversizedUrl)).toBeUndefined();
    }
  });
});

describe('YouTube embed', () => {
  it('preserves the wrapper, play button, and privacy-enhanced iframe contract', () => {
    const { container } = render(
      React.createElement(LiteYouTubeEmbed, {
        id: 'dQw4w9WgXcQ',
        title: 'YouTube fixture',
        wrapperClass: 'ff-youtube',
      })
    );

    expect(container.querySelector('.ff-youtube')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Watch YouTube fixture' }));

    const iframe = screen.getByTitle('YouTube fixture');
    expect(iframe).toHaveAttribute(
      'src',
      expect.stringContaining('https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ')
    );
  });
});
