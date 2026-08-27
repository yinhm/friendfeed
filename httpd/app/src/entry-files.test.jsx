import {render, screen} from '@testing-library/react';

import {EntryFiles} from './entry-files';

test('renders attachment names, sizes, and application download URLs', () => {
  render(<EntryFiles files={[{
    name: 'report.pdf', size: 1536,
    url: '/e/entry/files/digest/report.pdf',
  }]} />);
  const link = screen.getByRole('link', {name: /report\.pdf/});
  expect(link).toHaveAttribute('href', '/e/entry/files/digest/report.pdf');
  expect(link).toHaveTextContent('1.5 KB');
});

test('renders nothing without attachments', () => {
  const {container} = render(<EntryFiles files={[]} />);
  expect(container).toBeEmptyDOMElement();
});
