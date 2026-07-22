import React from 'react';
import {fireEvent, render, screen} from '@testing-library/react';
import {EntryLike} from './entry-like';

test('expands placeholder likes once when clicked', () => {
  const expandLikes = vi.fn();
  render(
    <EntryLike
      like={{placeholder: true, body: '12 other people'}}
      expandLikes={expandLikes}
    />
  );

  const button = screen.getByRole('button', {name: '12 other people'});
  fireEvent.click(button);
  fireEvent.click(button);

  expect(expandLikes).toHaveBeenCalledTimes(1);
});
