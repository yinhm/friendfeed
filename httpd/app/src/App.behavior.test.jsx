import React from 'react';
import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';

const {getJSONMock, postFormMock, postJSONMock} = vi.hoisted(() => ({
  getJSONMock: vi.fn(),
  postFormMock: vi.fn(),
  postJSONMock: vi.fn(),
}));

vi.mock('./utils', async (importOriginal) => ({
  ...(await importOriginal()),
  getJSON: getJSONMock,
  postForm: postFormMock,
  postJSON: postJSONMock,
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

  const {rerender, unmount} = render(<Feed {...makeFeedProps()} />);
  expect(screen.getByText('Old entry')).toBeInTheDocument();

  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });
  rerender(<Feed {...makeFeedProps({url: '/feed/latest?output=json'})} />);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });

  expect(getJSONMock).toHaveBeenCalledOnce();
  expect(getJSONMock).toHaveBeenCalledWith('/feed/latest?output=json');
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

test('Feed header follows and unfollows with semantic actions', async () => {
  postJSONMock.mockResolvedValue({});
  const feed = {
    id: 'friend-feed',
    uuid: 'feed-uuid',
    name: 'Friend Feed',
    commands: ['follow'],
    entries: [],
  };

  render(<Feed {...makeFeedProps({feed, show_header: true})} />);

  fireEvent.click(screen.getByRole('button', {name: 'Follow'}));
  await waitFor(() => {
    expect(postJSONMock).toHaveBeenCalledWith('/a/follow', {
      feed_uuid: 'feed-uuid',
      action: 'follow',
    });
  });
  expect(screen.getByRole('button', {name: 'Unfollow'})).toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', {name: 'Unfollow'}));
  await waitFor(() => {
    expect(postJSONMock).toHaveBeenLastCalledWith('/a/follow', {
      feed_uuid: 'feed-uuid',
      action: 'unfollow',
    });
  });
  expect(screen.getByRole('button', {name: 'Follow'})).toBeInTheDocument();
});

test('Feed header shows the error when unfollow is rejected', async () => {
  postJSONMock.mockRejectedValue(
    new Error('This action cannot be completed.'));
  const feed = {
    id: 'group-feed',
    uuid: 'group-uuid',
    name: 'Group Feed',
    commands: ['unfollow'],
    entries: [],
  };

  render(<Feed {...makeFeedProps({feed, show_header: true})} />);

  fireEvent.click(screen.getByRole('button', {name: 'Unfollow'}));

  const alert = await screen.findByRole('alert');
  expect(alert).toHaveTextContent('This action cannot be completed.');
  // The relationship did not change, so the button must not flip to Follow.
  expect(screen.getByRole('button', {name: 'Unfollow'})).toBeInTheDocument();
});
