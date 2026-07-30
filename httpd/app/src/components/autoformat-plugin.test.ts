/* Behavior tests for the Plate 53 input rules that replaced the v49
 * AutoformatPlugin: block conversions (headings, blockquote, code block, hr,
 * indent lists), the code-block guard, and the text substitutions mapped 1:1
 * from the old autoformat* rule data.
 */

import { describe, expect, it } from 'vitest';
import { createPlateEditor } from 'platejs/react';

import { plugins } from 'components/plate-plugins';
import { serializeEditorHtml } from 'components/plate-serialization';

const type = (editor: any, text: string) => {
  for (const ch of text) editor.tf.insertText(ch);
};

const createEditor = (value: any[], path = [0, 0], offset = 0): any => {
  const editor = createPlateEditor({ plugins, value } as any);
  editor.selection = {
    anchor: { offset, path },
    focus: { offset, path },
  };
  return editor;
};

const paragraph = (text = '') => ({ children: [{ text }], type: 'p' });

describe('markdown block input rules', () => {
  it.each([
    ['# ', 'h1'],
    ['## ', 'h2'],
    ['### ', 'h3'],
    ['#### ', 'h4'],
    ['##### ', 'h5'],
    ['###### ', 'h6'],
  ])('converts %s into %s', (input, typeName) => {
    const editor = createEditor([paragraph()]);
    type(editor, input);
    expect(editor.children[0]).toMatchObject({
      children: [{ text: '' }],
      type: typeName,
    });
  });

  it('wraps the current paragraph in a blockquote container on "> "', () => {
    const editor = createEditor([paragraph()]);
    type(editor, '> ');
    expect(editor.children[0]).toMatchObject({
      children: [{ children: [{ text: '' }], type: 'p' }],
      type: 'blockquote',
    });
  });

  it('converts a ``` fence into a code block and selects its first line', () => {
    const editor = createEditor([paragraph()]);
    type(editor, '```');
    expect(editor.children[0]).toMatchObject({
      children: [{ children: [{ text: '' }], type: 'code_line' }],
      type: 'code_block',
    });
    expect(editor.selection?.anchor.path).toEqual([0, 0, 0]);
  });

  it('converts a trailing ``` fence after text into a following code block', () => {
    const editor = createEditor([paragraph('notes``')], [0, 0], 7);
    type(editor, '`');
    expect(editor.children[0]).toMatchObject({
      children: [{ text: 'notes' }],
      type: 'p',
    });
    expect(editor.children[1]).toMatchObject({
      children: [{ children: [{ text: '' }], type: 'code_line' }],
      type: 'code_block',
    });
    expect(editor.selection?.anchor.path).toEqual([1, 0, 0]);
  });

  it('does not fire the code block fence away from the block end', () => {
    const editor = createEditor([paragraph('a``b')], [0, 0], 3);
    type(editor, '`');
    expect(editor.children[0]).toMatchObject({
      children: [{ text: 'a```b' }],
      type: 'p',
    });
  });

  it.each(['---', '___ '])(
    'converts %s into an hr node without leftover fence text',
    (input) => {
      const editor = createEditor([paragraph()]);
      type(editor, input);
      expect(editor.children[0]).toMatchObject({ type: 'hr' });
      expect(editor.children[0].children).toEqual([{ text: '' }]);
      expect(
        editor.children.some((node: any) => node.type === 'p')
      ).toBe(true);
    }
  );

  it.each([
    ['* ', 'disc'],
    ['- ', 'disc'],
    ['1. ', 'decimal'],
    ['1) ', 'decimal'],
  ])('converts %s into an indent list (%s)', (input, listStyleType) => {
    const editor = createEditor([paragraph()]);
    type(editor, input);
    expect(editor.children[0]).toMatchObject({
      listStyleType,
      type: 'p',
    });
  });

  it('never fires block conversions inside a code block', () => {
    const editor = createEditor(
      [
        {
          children: [{ children: [{ text: '' }], type: 'code_line' }],
          type: 'code_block',
        },
      ],
      [0, 0, 0]
    );
    type(editor, '# ');
    expect(editor.children[0]).toMatchObject({
      children: [{ children: [{ text: '# ' }], type: 'code_line' }],
      type: 'code_block',
    });
  });
});

describe('text substitution input rules', () => {
  it.each([
    ['->', '→'],
    ['<-', '←'],
    ['=>', '⇒'],
    ['a--', 'a—'],
    ['...', '…'],
    ['>>', '»'],
    ['<<', '«'],
    ['(c)', '©'],
    ['(tm)', '™'],
    ['&copy;', '©'],
    ['&sect;', '§'],
    ['1/2', '½'],
    ['!=', '≠'],
    ['%%', '‰'],
    ['^2', '²'],
    ['~3', '₃'],
  ])('replaces %s with %s', (input, output) => {
    const editor = createEditor([paragraph()]);
    type(editor, input);
    expect(editor.children[0].children[0]).toMatchObject({ text: output });
  });

  it('turns a straight quote pair into smart quotes', () => {
    const editor = createEditor([paragraph()]);
    type(editor, '"ab"');
    expect(editor.children[0].children[0]).toMatchObject({ text: '“ab”' });
  });

  it('prefers the arrow rule over the math rule for =>', () => {
    const editor = createEditor([paragraph()]);
    type(editor, '=>');
    expect(editor.children[0].children[0]).toMatchObject({ text: '⇒' });
  });
});

describe('blockquote container (v53)', () => {
  it('lifts an empty blockquote line out of the blockquote on break', () => {
    const editor = createEditor(
      [{ children: [{ children: [{ text: '' }], type: 'p' }], type: 'blockquote' }],
      [0, 0, 0]
    );
    editor.tf.insertBreak();
    expect(editor.children[0]).toMatchObject({
      children: [{ text: '' }],
      type: 'p',
    });
  });

  it('lifts the paragraph out of the blockquote on delete at start', () => {
    const editor = createEditor(
      [
        paragraph('x'),
        {
          children: [{ children: [{ text: 'ab' }], type: 'p' }],
          type: 'blockquote',
        },
      ],
      [1, 0, 0]
    );
    editor.tf.deleteBackward('character');
    expect(editor.children[1]).toMatchObject({
      children: [{ text: 'ab' }],
      type: 'p',
    });
  });

  it('splits the inner paragraph on a mid-text break', () => {
    const editor = createEditor(
      [
        {
          children: [{ children: [{ text: 'ab' }], type: 'p' }],
          type: 'blockquote',
        },
      ],
      [0, 0, 0],
      1
    );
    editor.tf.insertBreak();
    expect(editor.children[0]).toMatchObject({
      children: [
        { children: [{ text: 'a' }], type: 'p' },
        { children: [{ text: 'b' }], type: 'p' },
      ],
      type: 'blockquote',
    });
  });

  it('normalizes a legacy flat blockquote into the container shape on edit', () => {
    const editor = createEditor(
      [{ children: [{ text: 'old' }], type: 'blockquote' }],
      [0, 0],
      3
    );
    type(editor, '!');
    expect(editor.children[0]).toMatchObject({
      children: [{ children: [{ text: 'old!' }], type: 'p' }],
      type: 'blockquote',
    });
  });

  it('serializes the container blockquote with its nested paragraph', async () => {
    const editor = createEditor([
      {
        children: [{ children: [{ text: 'quoted' }], type: 'p' }],
        type: 'blockquote',
      },
    ]);
    const html = await serializeEditorHtml(editor);
    expect(html).toMatch(/<blockquote><p>.*quoted.*<\/p><\/blockquote>/);
  });

  it('still serializes a legacy flat blockquote read-compatibly', async () => {
    const editor = createEditor([
      { children: [{ text: 'legacy quote' }], type: 'blockquote' },
    ]);
    const html = await serializeEditorHtml(editor);
    expect(html).toContain('<blockquote>');
    expect(html).toContain('legacy quote');
  });
});

describe('code block key behavior (v53)', () => {
  const codeBlock = (lines: string[]) => ({
    children: lines.map((text) => ({
      children: [{ text }],
      type: 'code_line',
    })),
    type: 'code_block',
  });

  it('splits a code line on break', () => {
    const editor = createEditor([codeBlock(['ab'])], [0, 0, 0], 1);
    editor.tf.insertBreak();
    expect(editor.children[0]).toMatchObject({
      children: [
        { children: [{ text: 'a' }], type: 'code_line' },
        { children: [{ text: 'b' }], type: 'code_line' },
      ],
      type: 'code_block',
    });
  });

  it('keeps Backspace at the start of a non-empty first line inside the block', () => {
    const editor = createEditor([codeBlock(['ab'])], [0, 0, 0], 0);
    editor.tf.deleteBackward('character');
    expect(editor.children).toHaveLength(1);
    expect(editor.children[0]).toMatchObject(codeBlock(['ab']));
  });

  it('merges an empty code line into the previous line on Backspace', () => {
    const editor = createEditor([codeBlock(['ab', ''])], [0, 1, 0], 0);
    editor.tf.deleteBackward('character');
    expect(editor.children[0]).toMatchObject(codeBlock(['ab']));
  });

  it('unwraps the code block on Backspace at an empty first line', () => {
    const editor = createEditor(
      [paragraph('q'), codeBlock([''])],
      [1, 0, 0],
      0
    );
    editor.tf.deleteBackward('character');
    expect(editor.children[1]).toMatchObject({
      children: [{ text: '' }],
      type: 'p',
    });
  });
});
