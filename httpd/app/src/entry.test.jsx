import React from 'react';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';
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

test('renders archived interactions without an actor snapshot', () => {
  const {container} = render(
    <Entry
      entry={{
        id: 'entry-id',
        from: {id: 'entry-author', name: 'Entry Author'},
        body: 'Entry body',
        commands: [],
        likes: [{from: {}}],
        comments: [{id: 'legacy-comment', body: 'Archived comment', from: {}}],
      }}
      onpage_edit={false}
    />
  );

  expect(screen.getAllByText('Unknown')).toHaveLength(2);
  expect(screen.getByText('Archived comment')).toBeInTheDocument();
  expect(screen.queryByRole('link', {name: 'Unknown'})).not.toBeInTheDocument();
  expect(container.querySelector('.likes-icon')).toBeEmptyDOMElement();
  expect(container.querySelector('.comment-icon')).toBeEmptyDOMElement();
});

test('marks private Feed names in entry interactions', () => {
  render(
    <Entry
      entry={{
        id: 'private-entry',
        from: {id: 'author', name: 'Author', private: true},
        body: 'Private actors',
        commands: [],
        likes: [{from: {id: 'liker', name: 'Liker', private: true}}],
        comments: [{
          id: 'comment',
          body: 'Comment',
          from: {id: 'commenter', name: 'Commenter', private: true},
        }],
      }}
      onpage_edit={false}
    />
  );

  expect(screen.getAllByRole('img', {name: 'Private'})).toHaveLength(3);
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

test('requires confirmation before deleting a comment', async () => {
  const fetchMock = vi.fn().mockResolvedValue({ok: true, json: () => Promise.resolve({})});
  vi.stubGlobal('fetch', fetchMock);

  try {
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

    // First click only asks for confirmation.
    fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
    expect(screen.getByRole('button', {name: '确定'})).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();

    // Cancel restores the plain Delete button.
    fireEvent.click(screen.getByRole('button', {name: '取消'}));
    expect(screen.queryByRole('button', {name: '确定'})).not.toBeInTheDocument();

    // Confirm issues the delete request and removes the comment.
    fireEvent.click(screen.getByRole('button', {name: 'Delete'}));
    fireEvent.click(screen.getByRole('button', {name: '确定'}));
    expect(fetchMock).toHaveBeenCalledWith('/a/comment/delete', expect.anything());
    await waitFor(() => expect(
      screen.queryByText(/Moderated comment/, {selector: 'span'})
    ).not.toBeInTheDocument());
  } finally {
    vi.unstubAllGlobals();
  }
});
