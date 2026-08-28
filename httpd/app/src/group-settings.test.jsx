import React from 'react';
import {render, screen} from '@testing-library/react';
import {GroupSettingsPage} from './group-settings';

it('renders metadata form, active nav, and native delete confirmation', () => {
  render(<GroupSettingsPage data={{error: 'save failed', group: {
    id: 'book-club', name: 'Book Club', description: 'Reading', picture: 'https://example.com/p.png', private: true,
  }}} />);
  expect(screen.getByRole('alert')).toHaveTextContent('save failed');
  expect(screen.getByLabelText('Group Name *')).toHaveValue('Book Club');
  expect(screen.getByRole('link', {name: 'Settings'})).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('button', {name: 'Delete group'})).toHaveAttribute('popovertarget', 'delete-group-confirmation');
  expect(screen.getByRole('button', {name: 'Confirm delete', hidden: true}).closest('form')).toHaveAttribute('action', '/groups/book-club/delete');
  expect(screen.getByRole('img', {name: 'Private'})).toBeInTheDocument();
});
