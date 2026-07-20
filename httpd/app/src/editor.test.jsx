import {fireEvent, render, screen, waitFor} from '@testing-library/react';

import OnPageEditor from './editor';

test('legacy rawBody content loads and is submitted as JSON and HTML', async () => {
  const postEntry = vi.fn().mockResolvedValue(undefined);
  const rawBody = JSON.stringify([
    {
      id: 'legacy-paragraph',
      type: 'p',
      children: [{text: 'Legacy rich text', bold: true}],
    },
  ]);

  const {container} = render(
    <OnPageEditor
      id="legacy-entry"
      feedUuid="legacy-feed"
      content={rawBody}
      postEntry={postEntry}
    />
  );

  expect(container.querySelector('[contenteditable="true"]')).toHaveTextContent(
    'Legacy rich text'
  );
  fireEvent.click(screen.getByDisplayValue('发布'));

  await waitFor(() => expect(postEntry).toHaveBeenCalledOnce());
  const formData = postEntry.mock.calls[0][0];
  expect(JSON.parse(formData.get('rawBody'))).toMatchObject([
    {type: 'p', children: [{text: 'Legacy rich text', bold: true}]},
  ]);
  expect(formData.get('body')).toMatch(
    /<strong[^>]*>Legacy rich text<\/strong>/
  );
});

test('legacy HTML fallback still deserializes into editable content', () => {
  const {container} = render(
    <OnPageEditor
      id="legacy-html-entry"
      feedUuid="legacy-feed"
      content="<p><strong>Legacy HTML text</strong></p>"
      postEntry={vi.fn()}
    />
  );

  expect(container.querySelector('[contenteditable="true"]')).toHaveTextContent(
    'Legacy HTML text'
  );
});
