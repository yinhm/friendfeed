import { render } from '@testing-library/react';
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
});
