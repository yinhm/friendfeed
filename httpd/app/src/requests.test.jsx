import React from 'react';
import {render, screen} from '@testing-library/react';
import {RequestsPage} from './requests';

it('renders bounded request actions from server data', () => {
  render(<RequestsPage data={{private: true, requests: [{requester: {uuid: 'u', id: 'alice', name: 'Alice', private: true}, requested_at: 'now'}]}} />);
  expect(screen.getByRole('link', {name: 'Alice'})).toHaveAttribute('href', '/feed/alice');
  expect(screen.getByRole('button', {name: 'Approve'})).toHaveAttribute('value', 'approve');
  expect(screen.getByRole('img', {name: 'Private'})).toBeInTheDocument();
});
