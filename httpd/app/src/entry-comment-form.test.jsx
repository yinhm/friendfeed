import {fireEvent, render, screen} from '@testing-library/react';

import {EntryCommentForm} from './entry';

const noop = () => {};

test('initializes the textarea from commentBody', () => {
  render(
    <EntryCommentForm
      commentBody="preserved draft"
      onSubmitComment={noop}
      onCancelComment={noop}
    />
  );
  expect(screen.getByRole('textbox')).toHaveValue('preserved draft');
  expect(screen.getByRole('textbox')).toHaveAccessibleName('Comment');
});

test('labels an existing comment editor distinctly', () => {
  render(
    <EntryCommentForm
      commentId="comment-1"
      onSubmitComment={noop}
      onCancelComment={noop}
    />
  );

  expect(screen.getByRole('textbox')).toHaveAccessibleName('Edit comment');
});

test('submit calls onSubmitComment with id and current text, then clears', () => {
  const onSubmitComment = vi.fn();
  render(
    <EntryCommentForm
      commentId="comment-1"
      onSubmitComment={onSubmitComment}
      onCancelComment={noop}
    />
  );

  const textarea = screen.getByRole('textbox');
  fireEvent.change(textarea, {target: {value: 'edited body'}});
  fireEvent.click(screen.getByRole('button', {name: 'Post'}));

  expect(onSubmitComment).toHaveBeenCalledOnce();
  const [id, body] = onSubmitComment.mock.calls[0];
  expect(id).toBe('comment-1');
  expect(body).toBe('edited body');
  expect(textarea).toHaveValue('');
});

test('empty textarea blocks submission', () => {
  const onSubmitComment = vi.fn();
  render(<EntryCommentForm onSubmitComment={onSubmitComment} onCancelComment={noop} />);

  fireEvent.click(screen.getByRole('button', {name: 'Post'}));
  expect(onSubmitComment).not.toHaveBeenCalled();
});

test('cancel passes the current text to onCancelComment', () => {
  const onCancelComment = vi.fn();
  render(
    <EntryCommentForm
      commentBody="initial"
      onSubmitComment={noop}
      onCancelComment={onCancelComment}
    />
  );

  fireEvent.change(screen.getByRole('textbox'), {target: {value: 'changed'}});
  fireEvent.click(screen.getByRole('button', {name: 'Cancel'}));

  expect(onCancelComment).toHaveBeenCalledOnce();
  const [, body] = onCancelComment.mock.calls[0];
  expect(body).toBe('changed');
});
