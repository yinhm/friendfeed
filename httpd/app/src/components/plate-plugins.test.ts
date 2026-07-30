import { describe, expect, it } from 'vitest';
import { Hotkeys } from 'platejs';
import { createPlateEditor } from 'platejs/react';

import {
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_H4,
  ELEMENT_H5,
  ELEMENT_H6,
  ELEMENT_IMAGE,
  ELEMENT_MEDIA_EMBED,
  ELEMENT_PARAGRAPH,
  ELEMENT_TODO_LI,
  HEADING_KEYS,
} from 'components/plate-plugin-keys';
import { plugins } from 'components/plate-plugins';
import {
  H1Element,
  H2Element,
  H3Element,
  H4Element,
  H5Element,
  H6Element,
} from 'components/plate-ui/heading-element';
import { LinkElement } from 'components/plate-ui/link-element';
import { LinkFloatingToolbar } from 'components/plate-ui/link-floating-toolbar';

interface PluginSnapshot {
  inject?: {
    nodeProps?: Record<string, unknown>;
    targetPlugins?: string[];
  };
  key: string;
  node?: { component?: unknown };
  options?: unknown;
  plugins?: PluginSnapshot[];
  render?: { afterEditable?: unknown };
  rules?: unknown;
  shortcuts?: unknown;
}

const allPlugins = (items: readonly PluginSnapshot[]): PluginSnapshot[] =>
  items.flatMap((item) => [item, ...allPlugins(item.plugins ?? [])]);

const configuredPlugins = createPlateEditor({ plugins }).meta.pluginList;

const plugin = (key: string) => {
  const result = allPlugins(
    configuredPlugins as unknown as PluginSnapshot[]
  ).find(
    (item) => item.key === key
  );
  expect(result, `missing plugin ${key}`).toBeDefined();
  return result!;
};

const options = <T,>(key: string) => plugin(key).options as T;

describe('Plate plugin configuration', () => {
  it('keeps content and mark plugins registered', () => {
    expect(
      allPlugins(configuredPlugins as unknown as PluginSnapshot[]).map(
        ({ key }) => key
      )
    ).toEqual(
      expect.arrayContaining([
        ELEMENT_PARAGRAPH,
        ...HEADING_KEYS,
        ELEMENT_BLOCKQUOTE,
        ELEMENT_CODE_BLOCK,
        ELEMENT_IMAGE,
        ELEMENT_MEDIA_EMBED,
        'a',
        'bold',
        'italic',
        'underline',
        'strikethrough',
        'code',
      ])
    );
  });

  it('registers heading levels by key with per-level components', () => {
    const levelComponents = {
      [ELEMENT_H1]: H1Element,
      [ELEMENT_H2]: H2Element,
      [ELEMENT_H3]: H3Element,
      [ELEMENT_H4]: H4Element,
      [ELEMENT_H5]: H5Element,
      [ELEMENT_H6]: H6Element,
    } as const;
    for (const [key, component] of Object.entries(levelComponents)) {
      expect(plugin(key).node?.component, `component for ${key}`).toStrictEqual(
        component
      );
    }
  });

  it('keeps break, caption and trailing-block behavior', () => {
    expect(plugin('exitBreak').shortcuts).toMatchObject({
      insert: { keys: 'mod+enter' },
      insertBefore: { keys: 'mod+shift+enter' },
    });
    for (const key of HEADING_KEYS) {
      expect(plugin(key).rules).toMatchObject({
        break: { splitReset: true },
      });
    }
    expect(options<{ query: { allow: string[] } }>('caption').query).toEqual({
      allow: [ELEMENT_IMAGE, ELEMENT_MEDIA_EMBED],
    });
    expect(options<{ type: string }>('trailingBlock').type).toBe(
      ELEMENT_PARAGRAPH
    );
  });

  it('keeps block-style targets and values', () => {
    const textBlockTypes = [
      ELEMENT_PARAGRAPH,
      ELEMENT_H1,
      ELEMENT_H2,
      ELEMENT_H3,
      ELEMENT_BLOCKQUOTE,
      ELEMENT_CODE_BLOCK,
    ];
    expect(plugin('align').inject?.targetPlugins).toEqual(
      textBlockTypes.slice(0, 4)
    );
    expect(plugin('indent').inject?.targetPlugins).toEqual(textBlockTypes);
    expect(plugin('list').inject?.targetPlugins).toEqual(textBlockTypes);
    expect(plugin('lineHeight').inject).toMatchObject({
      nodeProps: {
        defaultNodeValue: 1.5,
        validNodeValues: [1, 1.2, 1.5, 2, 3],
      },
      targetPlugins: textBlockTypes.slice(0, 4),
    });
  });

  it('keeps per-node reset and line-break rules', () => {
    expect(plugin(ELEMENT_BLOCKQUOTE).rules).toMatchObject({
      break: { default: 'lineBreak', empty: 'reset' },
      delete: { start: 'reset' },
    });
    expect(plugin(ELEMENT_CODE_BLOCK).rules).toMatchObject({
      break: { default: 'lineBreak', empty: 'reset' },
      delete: { start: 'reset' },
    });
    expect(plugin(ELEMENT_TODO_LI).rules).toMatchObject({
      break: { empty: 'reset' },
      delete: { start: 'reset' },
    });
  });

  it('keeps core soft-break, void selection and node-id behavior', () => {
    expect(
      Hotkeys.isSoftBreak({ key: 'Enter', shiftKey: true } as KeyboardEvent)
    ).toBe(true);

    // The mapped hotkey performs a real soft break: a newline inside the
    // current block instead of a new block.
    const softBreakEditor = createPlateEditor({
      plugins,
      value: [
        { type: ELEMENT_PARAGRAPH, children: [{ text: 'ab' }] },
      ],
    });
    softBreakEditor.selection = {
      anchor: { path: [0, 0], offset: 1 },
      focus: { path: [0, 0], offset: 1 },
    };
    softBreakEditor.tf.insertSoftBreak();
    expect(softBreakEditor.children).toHaveLength(1);
    expect(softBreakEditor.children[0].children[0]).toMatchObject({
      text: 'a\nb',
    });

    const editor = createPlateEditor({
      nodeId: {},
      plugins,
      value: [
        { type: ELEMENT_PARAGRAPH, children: [{ text: 'before' }] },
        {
          type: ELEMENT_IMAGE,
          url: 'https://example.com/image.png',
          children: [{ text: '' }],
        },
        { type: ELEMENT_PARAGRAPH, children: [{ text: '' }] },
      ],
    });

    expect(editor.children.every((node) => 'id' in node)).toBe(true);

    editor.selection = {
      anchor: { path: [2, 0], offset: 0 },
      focus: { path: [2, 0], offset: 0 },
    };
    editor.tf.deleteBackward('character');

    expect(editor.children).toHaveLength(3);
    expect(editor.children[1]).toMatchObject({ type: ELEMENT_IMAGE });
    expect(editor.selection?.anchor.path[0]).toBe(1);
  });

  it('keeps tabbable and link UI integration', () => {
    expect(options<{ query: unknown }>('tabbable').query).toEqual(
      expect.any(Function)
    );
    expect(plugin('a').node?.component).toStrictEqual(LinkElement);
    const afterEditable = plugin('a').render?.afterEditable as () => {
      type: unknown;
    };
    expect(afterEditable()).toMatchObject({ type: LinkFloatingToolbar });
  });
});
