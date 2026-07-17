import { render, waitFor } from '@testing-library/react';
import { App } from './App';

test('renders the feed container from window.appData', () => {
  window.appData = {
    feed: {entries: []},
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

test('renders the sharing editor with React 19', async () => {
  window.appData = {
    feed: {uuid: 'public', entries: []},
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
  }, {timeout: 5000});
});
