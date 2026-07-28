import React from 'react';
import {fireEvent, render, screen} from '@testing-library/react';
import {Entry} from './entry';
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

test('renders only the comment commands authorized by the server', () => {
  render(
    <Entry
      entry={{
        id: 'entry-id',
        from: {id: 'entry-author', name: 'Entry Author'},
        body: 'Entry body',
        commands: [],
        comments: [{
          id: 'comment-id',
          body: 'Moderated comment',
          from: {id: 'comment-author', name: 'Comment Author'},
          commands: ['delete'],
        }],
      }}
      onpage_edit={false}
    />
  );

  expect(screen.getByRole('button', {name: 'Delete'})).toBeInTheDocument();
  expect(screen.queryByRole('button', {name: 'Edit'})).not.toBeInTheDocument();
});
