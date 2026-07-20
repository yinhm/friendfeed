import React from 'react';
import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';

const {getJSONMock, postFormMock} = vi.hoisted(() => ({
  getJSONMock: vi.fn(),
  postFormMock: vi.fn(),
}));

vi.mock('./utils', async (importOriginal) => ({
  ...(await importOriginal()),
  getJSON: getJSONMock,
  postForm: postFormMock,
}));

vi.mock('./editor', () => ({
  default: ({postEntry}) => (
    <button type="button" onClick={() => postEntry(new FormData())}>
      Submit test entry
    </button>
  ),
}));

import {Feed} from './App';

const makeEntry = (id, body) => ({
  id,
  body,
  commands: [],
  from: {id: 'author', name: 'Author'},
});

const makeFeedProps = (overrides = {}) => ({
  url: '/feed/home?output=json',
  feed: {id: 'home', uuid: 'home', entries: [makeEntry('old', 'Old entry')]},
  show_header: false,
  show_paging: false,
  show_share: false,
  prev_start: 0,
  next_start: 0,
  query: '',
  onpage: false,
  onpage_edit: false,
  ...overrides,
});

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
});

test('Feed refreshes after 20 seconds and stops polling after unmount', async () => {
  vi.useFakeTimers();
  getJSONMock.mockResolvedValue({
    ...makeFeedProps(),
    feed: {id: 'home', uuid: 'home', entries: [makeEntry('new', 'Fresh entry')]},
  });

  const {unmount} = render(<Feed {...makeFeedProps()} />);
  expect(screen.getByText('Old entry')).toBeInTheDocument();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(20_000);
  });

  expect(getJSONMock).toHaveBeenCalledOnce();
  expect(getJSONMock).toHaveBeenCalledWith('/feed/home?output=json');
  expect(screen.getByText('Fresh entry')).toBeInTheDocument();

  unmount();
  await vi.advanceTimersByTimeAsync(20_000);
  expect(getJSONMock).toHaveBeenCalledOnce();
});

test('Feed prepends a successfully posted entry', async () => {
  postFormMock.mockResolvedValue(makeEntry('new', 'Newest entry'));

  const {container} = render(
    <Feed {...makeFeedProps({show_share: true})} />
  );
  fireEvent.click(await screen.findByRole('button', {name: 'Submit test entry'}));

  await waitFor(() => {
    expect(postFormMock).toHaveBeenCalledWith('/a/share', expect.any(FormData));
  });
  expect(
    [...container.querySelectorAll('.entry .content')].map((node) => node.textContent)
  ).toEqual(['Newest entry', 'Old entry']);
});
