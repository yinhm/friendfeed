import React from 'react';
import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';

const {getJSONMock} = vi.hoisted(() => ({
  getJSONMock: vi.fn(),
}));

vi.mock('./utils', async (importOriginal) => ({
  ...(await importOriginal()),
  getJSON: getJSONMock,
}));

vi.mock('./editor', () => ({
  default: ({content}) => <div data-testid="test-editor">{content}</div>,
}));

import {Entry} from './entry';

const makeEntry = (overrides = {}) => ({
  id: 'entry-1',
  body: 'Original body',
  rawBody: 'original raw body',
  commands: [],
  from: {id: 'author', name: 'Author'},
  ...overrides,
});

afterEach(() => {
  vi.clearAllMocks();
});

test('Entry accepts refreshed props while it has no local edit in progress', () => {
  const {rerender} = render(
    <Entry entry={makeEntry()} onpage_edit={false} />
  );

  act(() => {
    rerender(
    <Entry
      entry={makeEntry({body: 'Refreshed body', rawBody: 'refreshed raw body'})}
      onpage_edit={false}
    />
  );
  });

  expect(screen.getByText('Refreshed body')).toBeInTheDocument();
  expect(screen.queryByText('Original body')).not.toBeInTheDocument();
});

test('Entry keeps expanded likes when refreshed props contain a collapsed list', async () => {
  getJSONMock.mockResolvedValue([
    {id: 'alice', name: 'Alice', from: {id: 'alice', name: 'Alice'}},
  ]);
  const {rerender} = render(
    <Entry
      entry={makeEntry({
        likes: [{placeholder: true, body: '1 other person', from: {id: '', name: ''}}],
      })}
      onpage_edit={false}
    />
  );

  fireEvent.click(screen.getByRole('button', {name: '1 other person'}));
  await waitFor(() => expect(screen.getByRole('link', {name: 'Alice'})).toBeInTheDocument());

  act(() => {
    rerender(
    <Entry
      entry={makeEntry({
        body: 'Refreshed body',
        likes: [{id: 'bob', name: 'Bob', from: {id: 'bob', name: 'Bob'}}],
      })}
      onpage_edit={false}
    />
  );
  });

  expect(screen.getByRole('link', {name: 'Alice'})).toBeInTheDocument();
  expect(screen.queryByRole('link', {name: 'Bob'})).not.toBeInTheDocument();
});

test('Entry keeps its original content while local editing is active', async () => {
  const {rerender} = render(
    <Entry entry={makeEntry({commands: ['edit']})} onpage_edit={false} />
  );
  fireEvent.click(screen.getByRole('button', {name: 'Edit'}));

  await waitFor(() => {
    expect(screen.getByTestId('test-editor')).toHaveTextContent('original raw body');
  });

  act(() => {
    rerender(
    <Entry
      entry={makeEntry({
        body: 'Refreshed body',
        rawBody: 'refreshed raw body',
        commands: ['edit'],
      })}
      onpage_edit={false}
    />
  );
  });

  expect(screen.getByTestId('test-editor')).toHaveTextContent('original raw body');
  expect(screen.getByTestId('test-editor')).not.toHaveTextContent('refreshed raw body');
});
