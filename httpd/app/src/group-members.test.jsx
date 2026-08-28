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
  expect(screen.getByRole('button', {name: 'Revoke admin'})).toHaveAttribute('value', 'demote');
  expect(screen.getByRole('button', {name: 'Make admin'})).toHaveAttribute('value', 'promote');
  expect(screen.getByRole('button', {name: 'Remove'})).toHaveAttribute('value', 'remove');
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
