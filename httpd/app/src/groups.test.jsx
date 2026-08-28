import React from 'react';
import {render, screen} from '@testing-library/react';
import {GroupsPage} from './groups';

it('renders discovery order, privacy, and cursor without relationship state', () => {
  render(<GroupsPage currentUserId="renamed-alice" data={{heading: 'Groups', page: 'discover',
    groups: [{id: 'alpha', name: 'Alpha'}, {id: 'secret', name: 'Secret', private: true}],
    empty_text: 'none', next_cursor: 'next/value',
  }} />);
  expect(screen.getAllByRole('link', {name: /Alpha|Secret/}).map(link => link.textContent)).toEqual(['Alpha', 'Secret']);
  expect(screen.getByRole('img', {name: 'Private'})).toBeInTheDocument();
  expect(screen.getByRole('link', {name: 'Discover'})).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', {name: 'My Groups'})).toHaveAttribute('href', '/feed/renamed-alice/groups');
  expect(screen.getByRole('link', {name: 'Next »'})).toHaveAttribute('href', '/groups?cursor=next%2Fvalue');
});

it('keeps anonymous discovery public without account navigation', () => {
  render(<GroupsPage data={{heading: 'Groups', page: 'discover', groups: [],
    empty_text: 'No groups are available yet.',
  }} />);
  expect(screen.getByText('No groups are available yet.')).toBeInTheDocument();
  expect(screen.queryByRole('navigation', {name: 'Group navigation'})).not.toBeInTheDocument();
});
