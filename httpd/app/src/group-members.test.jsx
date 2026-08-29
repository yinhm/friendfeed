import React from 'react';
import {render, screen} from '@testing-library/react';
import {GroupMembersPage} from './group-members';

const group = {id: 'book-club', name: 'Book Club', private: true};
const alice = {uuid: 'u1', id: 'alice', name: 'Alice'};
const bob = {uuid: 'u2', id: 'bob', name: 'Bob'};

it('renders bounded admin controls and pending requests from server data', () => {
  render(<GroupMembersPage data={{group, can_manage: true, has_more: true, error: '',
    members: [{profile: alice, is_admin: true}, {profile: bob, is_admin: false}],
    requests: [{requester: {uuid: 'u3', id: 'carol', name: 'Carol'}, requested_at: 'now'}],
  }} />);
  expect(screen.getByRole('link', {name: 'Members'})).toHaveAttribute('aria-current', 'page');
  const adminButtons = screen.getAllByRole('button', {name: 'Admin'});
  expect(screen.getByRole('link', {name: 'Alice'}).parentElement).toHaveClass('min-w-0', 'flex-1');
  expect(adminButtons[0].parentElement).toHaveClass('ml-auto', 'grid', 'grid-cols-2');
  expect(adminButtons[0]).toHaveClass('bg-white', 'border-stone-300');
  expect(adminButtons[0]).toHaveAttribute('popovertarget', 'revoke-admin-u1');
  expect(adminButtons[1]).toHaveClass('bg-[#1c1917]');
  expect(adminButtons[0]).toHaveClass('min-w-20');
  expect(adminButtons[1]).toHaveClass('min-w-20');
  expect(adminButtons[1]).toHaveAttribute('value', 'promote');
  expect(screen.getByRole('button', {name: 'Revoke', hidden: true})).toHaveAttribute('value', 'demote');
  const removeButtons = screen.getAllByRole('button', {name: 'Remove', hidden: true});
  expect(removeButtons[0]).toHaveAttribute('popovertarget', 'remove-member-u2');
  expect(removeButtons[1]).toHaveAttribute('value', 'remove');
  expect(screen.getByRole('button', {name: 'Approve'})).toHaveAttribute('value', 'approve');
  expect(screen.getByText('This group has more members than shown here.')).toBeInTheDocument();
});

it('does not infer management controls for a plain member', () => {
  render(<GroupMembersPage data={{group, can_manage: false, has_more: false, error: '',
    members: [{profile: bob, is_admin: false}], requests: [],
  }} />);
  expect(screen.queryByRole('button')).not.toBeInTheDocument();
  expect(screen.queryByRole('link', {name: 'Settings'})).not.toBeInTheDocument();
  expect(screen.queryByRole('link', {name: 'Import Services'})).not.toBeInTheDocument();
});
