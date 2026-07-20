import {act, fireEvent, render, screen, waitFor} from '@testing-library/react';
import {createPlateEditor} from '@udecode/plate-common/react';
import {serializeHtml} from '@udecode/plate-html/react';

import OnPageEditor from './editor';
import {plugins} from './components/plate-plugins';

test('typing the emoji trigger creates an inline emoji input', () => {
  const editor = createPlateEditor({plugins});
  editor.children = [{type: 'p', children: [{text: ''}]}];
  editor.selection = {
    anchor: {path: [0, 0], offset: 0},
    focus: {path: [0, 0], offset: 0},
  };

  editor.insertText(':');

  expect(editor.children[0].children).toEqual(
    expect.arrayContaining([
      expect.objectContaining({type: 'emoji_input'}),
    ])
  );
});

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

test('HTML serialization rejects unsafe link protocols', () => {
  const editor = createPlateEditor({plugins});
  editor.children = [
    {
      type: 'p',
      children: [
        {
          type: 'a',
          url: 'javascript:alert(1)',
          children: [{text: 'unsafe link'}],
        },
      ],
    },
  ];

  let html;
  act(() => {
    html = serializeHtml(editor, {nodes: editor.children});
  });

  expect(html).not.toMatch(/javascript:/i);
  expect(html).toContain('unsafe link');
});

test.each([
  'javascript:alert(1)',
  'data:text/html,<script>alert(1)</script>',
])('media serialization rejects unsafe URL %s', (url) => {
  const editor = createPlateEditor({plugins});
  editor.children = [
    {
      type: 'media_embed',
      url,
      children: [{text: ''}],
    },
  ];

  let html;
  act(() => {
    html = serializeHtml(editor, {nodes: editor.children});
  });

  expect(html).not.toMatch(/javascript:|data:text\/html|<script/i);
  expect(html).not.toContain('<iframe');
});
