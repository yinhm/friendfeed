import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Search } from './search';

describe('Search', () => {
  it('preserves the server GET /search?q= form contract', () => {
    const { container } = render(<Search />);
    const form = container.querySelector('form');
    const input = screen.getByRole('searchbox');

    expect(form).toHaveAttribute('action', '/search');
    expect(form).not.toHaveAttribute('method');
    expect(input).toHaveAttribute('name', 'q');

    fireEvent.change(input, { target: { value: 'friend feed' } });
    expect(input).toHaveValue('friend feed');
  });
});
