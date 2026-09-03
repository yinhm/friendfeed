import React from 'react';
import {render, screen} from '@testing-library/react';
import {GroupCreatePage} from './group-create';

it('renders submitted values, navigation, and the server validation error', () => {
  render(<GroupCreatePage currentUserId="alice" data={{error: 'ID taken', group: {
    id: 'book-club', name: 'Book Club', description: 'Reading', picture: 'https://example.com/p.png', private: true,
  }, picture_action: 'replace', picture_asset_token: 'avatar-token'}} />);
  expect(screen.getByRole('alert')).toHaveTextContent('ID taken');
  expect(screen.getByLabelText('Group ID *')).toHaveValue('book-club');
  expect(screen.getByLabelText('Group Name *')).toHaveValue('Book Club');
  expect(screen.getByLabelText('Private group')).toBeChecked();
  expect(screen.getByRole('link', {name: 'Create'})).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', {name: 'My Groups'})).toHaveAttribute('href', '/feed/alice/groups');
  expect(screen.getByRole('button', {name: 'Create'}).closest('form')).toHaveAttribute('action', '/groups/create');
  expect(document.querySelector('input[name="picture_action"]')).toHaveValue('replace');
  expect(document.querySelector('input[name="picture_asset_token"]')).toHaveValue('avatar-token');
});
