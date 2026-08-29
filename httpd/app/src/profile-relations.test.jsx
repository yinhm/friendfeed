import React from 'react';
import {render, screen} from '@testing-library/react';
import {ProfileRelationsPage} from './profile-relations';

it('renders a profile relationship list without counts or pagination', () => {
  render(<ProfileRelationsPage data={{
    profile: {id: 'alice', name: 'Alice'}, relation: 'following',
    profiles: [{id: 'bob', name: 'Bob'}, {id: 'private-user', name: 'Private user', private: true}],
  }} />);
  expect(screen.getByRole('link', {name: 'Feed'})).toHaveAttribute('href', '/feed/alice');
  expect(screen.getByRole('link', {name: 'Following'})).toHaveAttribute('aria-current', 'page');
  expect(screen.getByRole('link', {name: 'Followers'})).toHaveAttribute('href', '/feed/alice/followers');
  expect(screen.getByRole('link', {name: 'Bob'})).toHaveAttribute('href', '/feed/bob');
  expect(screen.getByLabelText('Private')).toBeInTheDocument();
  expect(screen.queryByText(/next/i)).not.toBeInTheDocument();
});
