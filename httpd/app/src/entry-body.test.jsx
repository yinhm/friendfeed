import {render, screen} from '@testing-library/react';

import {EntryBody} from './entry-body';

const rawBodyOf = (children) => JSON.stringify(children);

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
