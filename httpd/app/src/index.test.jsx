import { act, waitFor } from '@testing-library/react';

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

test('entry module mounts only existing roots', async () => {
  document.body.innerHTML = '<div id="root"></div>'; // no #search, like /e/* pages
  setAppData();

  await act(async () => {
    await import('./index'); // must not throw despite missing #search
  });
  await waitFor(() => {
    expect(document.querySelector('#root #feed')).toBeInTheDocument();
  });
});
