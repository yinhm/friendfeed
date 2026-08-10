import { render, waitFor } from '@testing-library/react';
import { App } from './App';

test.each([
  ['Public', {id: 'public', uuid: 'public'}],
  ['Feed', {id: 'friend-feed', uuid: 'feed-uuid'}],
])('%s page does not render the post editor', (_page, feed) => {
  window.appData = {
    feed: {...feed, entries: []},
    show_header: false,
    show_paging: false,
    show_share: false,
    prev_start: 0,
    next_start: 0,
    query: '',
    onpage: false,
    onpage_edit: false,
  };

  const {container} = render(<App />);
  expect(container.querySelector('#feed')).toBeInTheDocument();
  expect(container.querySelector('[role="status"]')).not.toBeInTheDocument();
  expect(container.querySelector('[contenteditable="true"]')).not.toBeInTheDocument();
});

test('Home page renders the post editor input and submit control', async () => {
  window.appData = {
    feed: {id: 'home', uuid: 'home', entries: []},
    show_header: false,
    show_paging: false,
    show_share: true,
    prev_start: 0,
    next_start: 0,
    query: '',
    onpage: false,
    onpage_edit: false,
  };

  const {container} = render(<App />);
  expect(container.querySelector('[role="status"]')).toHaveTextContent('Loading editor…');
  // the editor is lazy-loaded, wait for the chunk to resolve
  await waitFor(() => {
    expect(container.querySelector('[contenteditable="true"]')).toBeInTheDocument();
  }, {timeout: 10000});
  expect(container.querySelector('button.submit[type="button"]')).toHaveTextContent('发布');
}, 10000);

test('Cursor-paged feeds render only the opaque next link', () => {
  window.appData = {
    feed: {id: 'friend-feed', uuid: 'feed-uuid', entries: []},
    show_header: false,
    show_paging: true,
    show_share: false,
    prev_start: 0,
    next_start: 0,
    cursor_paging: true,
    next_cursor: 'older+cursor',
    query: '',
    onpage: false,
    onpage_edit: false,
  };

  const {getByText, queryByText} = render(<App />);
  expect(queryByText('« Prev')).not.toBeInTheDocument();
  expect(getByText('Next »')).toHaveAttribute(
    'href', '?cursor=older%2Bcursor');
});
