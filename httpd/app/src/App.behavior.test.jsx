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

class MockEventSource {
  static instances = [];

  constructor(url) {
    this.url = url;
    this.closed = false;
    this.listeners = new Map();
    MockEventSource.instances.push(this);
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  emit(type) {
    this.listeners.get(type)?.(new Event(type));
  }

  close() {
    this.closed = true;
  }
}

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
  realtime_enabled: true,
  realtime_home: false,
  query: '',
  onpage: false,
  onpage_edit: false,
  ...overrides,
});

const newestHomeResponse = (entry = makeEntry('new', 'Fresh entry')) => ({
  ...makeFeedProps({realtime_home: true}),
  feed: {id: 'home', uuid: 'home', entries: [entry]},
});

function setVisibility(value) {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    value,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

beforeEach(() => {
  MockEventSource.instances = [];
  globalThis.EventSource = MockEventSource;
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    value: 'visible',
  });
});

afterEach(() => {
  vi.clearAllMocks();
  vi.useRealTimers();
  delete globalThis.EventSource;
});

test('non-home Feed keeps realtime hints but does not retain legacy polling', async () => {
  vi.useFakeTimers();
  render(<Feed {...makeFeedProps()} />);

  act(() => MockEventSource.instances[0].emit('timeline-dirty'));

  await act(async () => {
    await vi.advanceTimersByTimeAsync(180_000);
  });
  expect(screen.queryByRole('button', {name: '有新动态，点击刷新'})).not.toBeInTheDocument();
  expect(getJSONMock).not.toHaveBeenCalled();
});

test('anonymous Feed does not open the authenticated realtime stream', async () => {
  vi.useFakeTimers();
  render(<Feed {...makeFeedProps({realtime_enabled: false, realtime_home: true})} />);

  expect(MockEventSource.instances).toHaveLength(0);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(180_000);
  });
  expect(getJSONMock).not.toHaveBeenCalled();
});

test('notification dirty hint reveals the sidebar icon without fetching a count', () => {
  document.body.innerHTML = '<span id="notification-badge" class="notification-badge" hidden></span>';
  render(<Feed {...makeFeedProps()} />);

  act(() => MockEventSource.instances[0].emit('notifications-dirty'));

  const badge = document.getElementById('notification-badge');
  expect(badge).not.toHaveAttribute('hidden');
  expect(badge).toBeEmptyDOMElement();
  expect(badge).toHaveAttribute('title', 'New notifications');
  expect(getJSONMock).not.toHaveBeenCalled();
});

test('realtime Home folds dirty hints and refreshes newest page without cursor', async () => {
  getJSONMock.mockResolvedValue(newestHomeResponse());
  const {container} = render(<Feed {...makeFeedProps({
    realtime_home: true,
    show_share: true,
    url: '/?cursor=older-position',
  })} />);

  const editor = await screen.findByRole('button', {name: 'Submit test entry'});

  act(() => {
    MockEventSource.instances[0].emit('timeline-dirty');
    MockEventSource.instances[0].emit('timeline-dirty');
  });
  expect(screen.getAllByRole('status')).toHaveLength(1);
  expect(screen.getByRole('button', {name: '有新动态，点击刷新'})).toBeInTheDocument();
  const notification = container.querySelector('.notification.home-dirty-banner');
  const entry = container.querySelector('.entry');
  expect(notification).not.toBeNull();
  expect(editor.compareDocumentPosition(notification) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0);
  expect(notification.compareDocumentPosition(entry) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0);

  fireEvent.click(screen.getByRole('button', {name: '有新动态，点击刷新'}));
  await waitFor(() => expect(getJSONMock).toHaveBeenCalledOnce());
  expect(getJSONMock).toHaveBeenCalledWith('/');
  expect(await screen.findByText('Fresh entry')).toBeInTheDocument();
  expect(screen.queryByRole('button', {name: '有新动态，点击刷新'})).not.toBeInTheDocument();
});

test('failed realtime refresh keeps the dirty banner', async () => {
  getJSONMock.mockRejectedValue(new Error('offline'));
  render(<Feed {...makeFeedProps({realtime_home: true})} />);
  act(() => MockEventSource.instances[0].emit('timeline-dirty'));

  fireEvent.click(screen.getByRole('button', {name: '有新动态，点击刷新'}));
  await waitFor(() => expect(getJSONMock).toHaveBeenCalledOnce());
  expect(screen.getByRole('button', {name: '有新动态，点击刷新'})).toBeInTheDocument();
});

test('visible Home reconciles after returning from a hidden tab', async () => {
  getJSONMock.mockResolvedValue(newestHomeResponse());
  render(<Feed {...makeFeedProps({realtime_home: true})} />);
  const first = MockEventSource.instances[0];
  act(() => setVisibility('hidden'));
  expect(first.closed).toBe(true);
  expect(getJSONMock).not.toHaveBeenCalled();

  await act(async () => setVisibility('visible'));
  expect(MockEventSource.instances).toHaveLength(2);
  await waitFor(() => expect(getJSONMock).toHaveBeenCalledWith('/'));
});

test('visible realtime Home reconciles every 180 seconds and stops after unmount', async () => {
  vi.useFakeTimers();
  getJSONMock.mockResolvedValue(newestHomeResponse());
  const {unmount} = render(<Feed {...makeFeedProps({realtime_home: true})} />);

  await act(async () => {
    await vi.advanceTimersByTimeAsync(179_999);
  });
  expect(getJSONMock).not.toHaveBeenCalled();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(getJSONMock).toHaveBeenCalledOnce();
  expect(getJSONMock).toHaveBeenCalledWith('/');

  unmount();
  await vi.advanceTimersByTimeAsync(180_000);
  expect(getJSONMock).toHaveBeenCalledOnce();
  expect(screen.queryByRole('button', {name: '有新动态，点击刷新'})).not.toBeInTheDocument();
  expect(MockEventSource.instances[0].closed).toBe(true);
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

test('private Feed header marks its name with a lock icon', () => {
  const feed = {
    id: 'private-feed',
    uuid: 'private-uuid',
    name: 'Private Feed',
    private: true,
    entries: [],
  };

  render(<Feed {...makeFeedProps({feed, show_header: true})} />);

  expect(screen.getByRole('heading', {name: /^Private Feed/}))
    .toContainElement(screen.getByRole('img', {name: 'Private'}));
});

test('Feed management nav preserves server-authorized links after React mounts', () => {
  const feed = {
    id: 'book-club',
    uuid: 'group-uuid',
    name: 'Book Club',
    entries: [],
  };
  render(<Feed {...makeFeedProps({
    feed,
    show_header: true,
    manage_services_url: '/feed/book-club/import',
    group_settings_url: '/groups/book-club/settings',
    group_members_url: '/groups/book-club/members',
  })} />);

  const navigation = screen.getByRole('navigation', {name: 'Feed management'});
  expect(navigation).toBeInTheDocument();
  expect(navigation.closest('.header')).not.toBeNull();
  expect(screen.getByRole('link', {name: 'Feed'})).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', {name: 'Import Services'}))
    .toHaveAttribute('href', '/feed/book-club/import');
  expect(screen.getByRole('link', {name: 'Settings'}))
    .toHaveAttribute('href', '/groups/book-club/settings');
  expect(screen.getByRole('link', {name: 'Members'}))
    .toHaveAttribute('href', '/groups/book-club/members');
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
