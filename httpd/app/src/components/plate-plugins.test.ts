import { describe, expect, it } from 'vitest';
import { createPlateEditor } from '@udecode/plate-common/react';

import {
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_IMAGE,
  ELEMENT_MEDIA_EMBED,
  ELEMENT_PARAGRAPH,
  ELEMENT_TODO_LI,
  HEADING_KEYS,
} from 'components/plate-plugin-keys';
import { plugins } from 'components/plate-plugins';
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
}

const allPlugins = (items: readonly PluginSnapshot[]): PluginSnapshot[] =>
  items.flatMap((item) => [item, ...allPlugins(item.plugins ?? [])]);

const configuredPlugins = createPlateEditor({ plugins }).pluginList;

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
        'heading',
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

  it('keeps break, caption and trailing-block behavior', () => {
    expect(options<{ rules: unknown[] }>('exitBreak').rules).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ hotkey: 'mod+enter' }),
        expect.objectContaining({
          hotkey: 'enter',
          query: { allow: HEADING_KEYS, end: true, start: true },
        }),
      ])
    );
    expect(options<{ plugins: { key: string }[] }>('caption').plugins).toEqual([
      expect.objectContaining({ key: ELEMENT_IMAGE }),
      expect.objectContaining({ key: ELEMENT_MEDIA_EMBED }),
    ]);
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
    expect(plugin('listStyleType').inject?.targetPlugins).toEqual(textBlockTypes);
    expect(plugin('lineHeight').inject).toMatchObject({
      nodeProps: {
        defaultNodeValue: 1.5,
        validNodeValues: [1, 1.2, 1.5, 2, 3],
      },
      targetPlugins: textBlockTypes.slice(0, 4),
    });
  });

  it('keeps reset, soft-break and selection rules', () => {
    expect(options<{ rules: unknown[] }>('resetNode').rules).toEqual([
      expect.objectContaining({
        defaultType: ELEMENT_PARAGRAPH,
        hotkey: 'Enter',
        types: [ELEMENT_BLOCKQUOTE, ELEMENT_TODO_LI],
      }),
      expect.objectContaining({
        defaultType: ELEMENT_PARAGRAPH,
        hotkey: 'Backspace',
        types: [ELEMENT_BLOCKQUOTE, ELEMENT_TODO_LI],
      }),
      expect.objectContaining({
        defaultType: ELEMENT_PARAGRAPH,
        hotkey: 'Enter',
        types: [ELEMENT_CODE_BLOCK],
      }),
      expect.objectContaining({
        defaultType: ELEMENT_PARAGRAPH,
        hotkey: 'Backspace',
        types: [ELEMENT_CODE_BLOCK],
      }),
    ]);
    expect(options<{ rules: unknown[] }>('softBreak').rules).toEqual([
      { hotkey: 'shift+enter' },
      {
        hotkey: 'enter',
        query: { allow: [ELEMENT_CODE_BLOCK, ELEMENT_BLOCKQUOTE] },
      },
    ]);
    expect(options<{ query: unknown }>('selectOnBackspace').query).toEqual({
      allow: [ELEMENT_IMAGE],
    });
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
