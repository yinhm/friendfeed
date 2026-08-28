import React from 'react';
import {render, screen} from '@testing-library/react';
import {NotificationsPage} from './notifications';

it('renders notification snapshots and cursor paging', () => {
  render(<NotificationsPage data={{items: [{text: 'Alice liked your post', href: '/e/entry', date: '2026-08-28 10:00'}], next_cursor: 'a+b'}} />);
  expect(screen.getByRole('link', {name: 'Alice liked your post'})).toHaveAttribute('href', '/e/entry');
  expect(screen.getByRole('link', {name: 'Older notifications'})).toHaveAttribute('href', '/notifications?cursor=a%2Bb');
});

it('renders the empty state', () => {
  render(<NotificationsPage data={{items: []}} />);
  expect(screen.getByText('No notifications yet.')).toBeInTheDocument();
});
