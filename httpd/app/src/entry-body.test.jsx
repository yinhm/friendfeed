import {render, screen} from '@testing-library/react';

import {EntryBody} from './entry-body';

const rawBodyOf = (children) => JSON.stringify(children);

const longEntry = rawBodyOf([
  {
    type: 'p',
    children: [
      {text: 'a'.repeat(200)},
      {text: 'b'.repeat(200), bold: true},
    ],
  },
]);

test('truncates long rawBody in feed lists and links to the entry page', () => {
  render(
    <EntryBody rawBody={longEntry} body="" truncate entryId="entry-42" />
  );

  expect(screen.queryByText('b'.repeat(200))).not.toBeInTheDocument();
  const link = screen.getByRole('link', {name: 'Read more...'});
  expect(link).toHaveAttribute('href', '/e/entry-42');
});

test('renders the full value on the entry page (no truncate)', () => {
  render(<EntryBody rawBody={longEntry} body="" entryId="entry-42" />);

  expect(screen.getByText('b'.repeat(200))).toBeInTheDocument();
  expect(screen.queryByRole('link', {name: 'Read more...'})).not.toBeInTheDocument();
});

test('short rawBody is never truncated', () => {
  render(
    <EntryBody
      rawBody={rawBodyOf([{type: 'p', children: [{text: 'short'}]}])}
      body=""
      truncate
      entryId="entry-42"
    />
  );

  expect(screen.getByText('short')).toBeInTheDocument();
  expect(screen.queryByRole('link', {name: 'Read more...'})).not.toBeInTheDocument();
});

test('renders rawBody with the static component map', () => {
  render(
    <EntryBody
      rawBody={rawBodyOf([
        {
          type: 'p',
          children: [
            {text: 'rich '},
            {text: 'bold', bold: true},
            {
              type: 'a',
              url: 'https://example.com',
              children: [{text: ' link'}],
            },
          ],
        },
        {type: 'ul', children: [{type: 'li', children: [{text: 'item'}]}]},
      ])}
      body=""
    />
  );

  expect(screen.getByText('bold').tagName).toBe('STRONG');
  // Accessible names are whitespace-normalized, so " link" computes as "link".
  const link = screen.getByRole('link', {name: 'link'});
  expect(link).toHaveAttribute('href', 'https://example.com');
  expect(screen.getByText('item').closest('ul')).not.toBeNull();
});

test('drops unsafe link wrappers but keeps their text', () => {
  const {container} = render(
    <EntryBody
      rawBody={rawBodyOf([
        {
          type: 'p',
          children: [
            {
              type: 'a',
              url: 'javascript:alert(1)',
              children: [{text: 'unsafe link'}],
            },
          ],
        },
      ])}
      body=""
    />
  );

  expect(screen.getByText('unsafe link')).toBeInTheDocument();
  expect(container.querySelector('a')).toBeNull();
});

test('falls back to the sanitized HTML body when rawBody is missing or invalid', () => {
  const {container, rerender} = render(
    <EntryBody body="<p>legacy <strong>html</strong></p>" />
  );
  expect(container.querySelector('.content strong')).not.toBeNull();

  rerender(<EntryBody rawBody="not json" body="<p>legacy</p>" />);
  expect(screen.getByText('legacy')).toBeInTheDocument();
});

test.each([
  ['null node', '[null]'],
  ['scalar node', '["text"]'],
  ['children not an array', '[{"type":"p","children":{}}]'],
  ['nested invalid child', '[{"type":"p","children":[{"type":"p","children":[null]}]}]'],
  ['node without type or text', '[{"foo":"bar"}]'],
])('falls back to the HTML body for malformed rawBody: %s', (_label, rawBody) => {
  const {container} = render(
    <EntryBody rawBody={rawBody} body="<p>safe fallback</p>" />
  );
  expect(screen.getByText('safe fallback')).toBeInTheDocument();
  expect(container.querySelector('.content')).not.toBeNull();
});
