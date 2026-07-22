import axe from 'axe-core';
import { render } from '@testing-library/react';

import { App } from './App';
import { Search } from './search';

async function expectNoAccessibilityViolations(container) {
  const result = await axe.run(container, {
    rules: {
      // jsdom has no layout or canvas implementation, so color contrast cannot
      // be measured reliably in this unit-test environment.
      'color-contrast': { enabled: false },
    },
  });

  expect(result.violations, JSON.stringify(result.violations, null, 2)).toEqual(
    []
  );
}

test('public feed has no detectable accessibility violations', async () => {
  window.appData = {
    feed: { id: 'public', uuid: 'public', entries: [] },
    show_header: false,
    show_paging: false,
    show_share: false,
    prev_start: 0,
    next_start: 0,
    query: '',
    onpage: false,
    onpage_edit: false,
  };

  const { container } = render(<App />);
  await expectNoAccessibilityViolations(container);
});

test('search form has no detectable accessibility violations', async () => {
  const { container } = render(<Search />);
  await expectNoAccessibilityViolations(container);
});
