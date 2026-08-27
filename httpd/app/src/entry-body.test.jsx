import {render} from '@testing-library/react';

import {EntryBody} from './entry-body';

test('renders the sanitized HTML body as-is', () => {
  const {container} = render(<EntryBody body='<p>rich <strong>text</strong></p>' />);

  expect(container.querySelector('.content')?.innerHTML).toBe('<p>rich <strong>text</strong></p>');
});

test('list bodies arrive truncated from the server, including the read-more link', () => {
  const {container} = render(
    <EntryBody body='<p>cut</p><a href="/e/entry-42" style="padding-left: 30px;">Read more...</a>' />
  );

  expect(container.querySelector('a')?.getAttribute('href')).toBe('/e/entry-42');
});
