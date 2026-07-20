import { describe, expect, it } from 'vitest';

import {
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_IMAGE,
  ELEMENT_MEDIA_EMBED,
  ELEMENT_PARAGRAPH,
  HEADING_KEYS,
} from 'components/plate-plugin-keys';
import { plugins } from 'components/plate-plugins';
import { LinkElement } from 'components/plate-ui/link-element';
import { LinkFloatingToolbar } from 'components/plate-ui/link-floating-toolbar';

type Plugin = (typeof plugins)[number] & { plugins?: Plugin[] };

const allPlugins = (items: Plugin[]): Plugin[] =>
  items.flatMap((plugin) => [plugin, ...allPlugins(plugin.plugins ?? [])]);

const plugin = (key: string) => {
  const result = allPlugins(plugins as Plugin[]).find((item) => item.key === key);
  expect(result, `missing plugin ${key}`).toBeDefined();
  return result!;
};

describe('Plate plugin configuration', () => {
  it('keeps the content and mark plugins registered', () => {
    expect(allPlugins(plugins as Plugin[]).map(({ key }) => key)).toEqual(
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
    expect(plugin('exitBreak').options?.rules).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ hotkey: 'mod+enter' }),
        expect.objectContaining({
          hotkey: 'enter',
          query: { allow: HEADING_KEYS, end: true, start: true },
        }),
      ])
    );
    expect(plugin('caption').options?.pluginKeys).toEqual([
      ELEMENT_IMAGE,
      ELEMENT_MEDIA_EMBED,
    ]);
    expect(plugin('trailingBlock').options?.type).toBe(ELEMENT_PARAGRAPH);
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

    expect(plugin('align').inject?.props?.validTypes).toEqual([
      ELEMENT_PARAGRAPH,
      ELEMENT_H1,
      ELEMENT_H2,
      ELEMENT_H3,
    ]);
    expect(plugin('indent').inject?.props?.validTypes).toEqual(textBlockTypes);
    expect(plugin('listStyleType').inject?.props?.validTypes).toEqual(
      textBlockTypes
    );
    expect(plugin('lineHeight').inject?.props).toMatchObject({
      defaultNodeValue: 1.5,
      validNodeValues: [1, 1.2, 1.5, 2, 3],
      validTypes: [ELEMENT_PARAGRAPH, ELEMENT_H1, ELEMENT_H2, ELEMENT_H3],
    });
  });

  it('keeps reset, soft-break and selection rules', () => {
    expect(plugin('resetNode').options?.rules).toEqual([
      expect.objectContaining({
        defaultType: ELEMENT_PARAGRAPH,
        hotkey: 'Enter',
        types: [ELEMENT_BLOCKQUOTE, 'action_item'],
      }),
      expect.objectContaining({
        defaultType: ELEMENT_PARAGRAPH,
        hotkey: 'Backspace',
        types: [ELEMENT_BLOCKQUOTE, 'action_item'],
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
    expect(plugin('softBreak').options?.rules).toEqual([
      { hotkey: 'shift+enter' },
      {
        hotkey: 'enter',
        query: { allow: [ELEMENT_CODE_BLOCK, ELEMENT_BLOCKQUOTE] },
      },
    ]);
    expect(plugin('selectOnBackspace').options?.query).toEqual({
      allow: [ELEMENT_IMAGE],
    });
  });

  it('keeps tabbable and link UI integration', () => {
    expect(plugin('tabbable').options?.query).toEqual(expect.any(Function));
    expect(plugin('a')).toMatchObject({
      component: LinkElement,
      renderAfterEditable: LinkFloatingToolbar,
    });
  });
});
