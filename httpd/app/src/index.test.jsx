import { waitFor } from '@testing-library/react';
import { vi } from 'vitest';

// Regression test for the entry-page crash: on pages without the sidebar
// there is no #search element; createRoot(null) threw, which marked the
// entry module as errored and then broke lazy chunks importing it.
function setAppData() {
  window.appData = {
    feed: {entries: []},
    show_header: false,
    show_paging: false,
    show_share: false,
    prev_start: 0,
    next_start: 0,
    query: '',
    onpage: true,
    onpage_edit: false,
  };
}

test('entry module mounts only existing roots and is idempotent', async () => {
  document.body.innerHTML = '<div id="root"></div>'; // no #search, like /e/* pages
  setAppData();

  await import('./index'); // must not throw despite missing #search
  await waitFor(() => {
    expect(document.querySelector('#root #feed')).toBeInTheDocument();
  });

  // production: chunk imports ./index.js while the page loaded bundle.min.js,
  // so the module can be evaluated a second time — must stay a no-op
  vi.resetModules();
  await import('./index');
  expect(document.querySelectorAll('#root #feed')).toHaveLength(1);
});
