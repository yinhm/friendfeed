import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import {createPlateEditor} from 'platejs/react';

import OnPageEditor from './editor';
import {plugins} from './components/plate-plugins';
import {serializeEditorHtml} from './components/plate-serialization';

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

test('stored font marks round-trip through load and serialize', async () => {
  // Font color/backgroundColor/size plugins are intentionally unregistered:
  // the marks must survive as plain data in rawBody even though nothing
  // renders them.
  const editor = createPlateEditor({
    plugins,
    value: [
      {
        type: 'p',
        children: [
          {
            text: 'styled text',
            color: '#ff0000',
            backgroundColor: '#ffff00',
            fontSize: '18px',
          },
        ],
      },
    ],
  });

  expect(editor.children[0].children[0]).toMatchObject({
    text: 'styled text',
    color: '#ff0000',
    backgroundColor: '#ffff00',
    fontSize: '18px',
  });

  const html = await serializeEditorHtml(editor);
  expect(html).toContain('styled text');
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
  fireEvent.click(screen.getByRole('button', {name: '发布'}));

  await waitFor(() => expect(postEntry).toHaveBeenCalledOnce());
  const formData = postEntry.mock.calls[0][0];
  expect(JSON.parse(formData.get('rawBody'))).toMatchObject([
    {type: 'p', children: [{text: 'Legacy rich text', bold: true}]},
  ]);
  expect(formData.get('body')).toMatch(
    /<strong[^>]*>.*Legacy rich text.*<\/strong>/
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

test('renders each heading level with its per-level size variant', async () => {
  const rawBody = JSON.stringify([
    {type: 'h1', children: [{text: 'Title'}]},
    {type: 'h2', children: [{text: 'Section'}]},
    {type: 'h3', children: [{text: 'Subsection'}]},
  ]);

  const {container} = render(
    <OnPageEditor
      id="heading-entry"
      feedUuid="heading-feed"
      content={rawBody}
      postEntry={vi.fn()}
    />
  );

  await waitFor(() => {
    expect(container.querySelector('h2')).toHaveTextContent('Section');
  });
  expect(container.querySelector('h1').className).toContain('text-4xl');
  expect(container.querySelector('h2').className).toContain('text-2xl');
  expect(container.querySelector('h3').className).toContain('text-xl');
});

test('HTML serialization rejects unsafe link protocols', async () => {
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
  const html = await serializeEditorHtml(editor);

  expect(html).not.toMatch(/javascript:/i);
  expect(html).toContain('unsafe link');
});

test.each([
  'javascript:alert(1)',
  'data:text/html,<script>alert(1)</script>',
])('media serialization rejects unsafe URL %s', async (url) => {
  const editor = createPlateEditor({plugins});
  editor.children = [
    {
      type: 'media_embed',
      url,
      children: [{text: ''}],
    },
  ];
  const html = await serializeEditorHtml(editor);

  expect(html).not.toMatch(/javascript:|data:text\/html|<script/i);
  expect(html).not.toContain('<iframe');
});

test('legacy plain-text rawBody (pre-JSON imports) renders without crashing', async () => {
  // Entry e3c3aada-dc05-58f5-a0c8-9b6f1302d8c2: rawBody stored as plain text,
  // which deserialized to a root-level text node and crashed the editor.
  const rawBody = '差评。真正洁癖的拿方便面不会带出一滴碎渣。 https://t.co/FunM4aMYa5';

  const {container} = render(
    <OnPageEditor
      id="legacy-plain"
      feedUuid="legacy-feed"
      content={rawBody}
      postEntry={vi.fn()}
    />
  );

  await waitFor(() => {
    expect(container.querySelector('[contenteditable="true"]')).toBeInTheDocument();
  });
  expect(container.querySelector('[contenteditable="true"]')).toHaveTextContent('差评');
});
