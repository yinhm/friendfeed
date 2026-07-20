import {
  MARK_BOLD,
  MARK_CODE,
  MARK_ITALIC,
  MARK_STRIKETHROUGH,
  MARK_SUBSCRIPT,
  MARK_SUPERSCRIPT,
  MARK_UNDERLINE,
} from '@udecode/plate-basic-marks';
import { ELEMENT_BLOCKQUOTE } from '@udecode/plate-block-quote';
import {
  ELEMENT_CODE_BLOCK,
  ELEMENT_CODE_LINE,
  ELEMENT_CODE_SYNTAX,
} from '@udecode/plate-code-block';
import { ELEMENT_EMOJI_INPUT } from '@udecode/plate-emoji';
import { ELEMENT_DEFAULT } from '@udecode/plate-common';
import {
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_H4,
  ELEMENT_H5,
  ELEMENT_H6,
  KEYS_HEADING,
} from '@udecode/plate-heading';
import { MARK_HIGHLIGHT } from '@udecode/plate-highlight';
import { KEY_LIST_STYLE_TYPE } from '@udecode/plate-indent-list';
import { ELEMENT_HR } from '@udecode/plate-horizontal-rule';
import { ELEMENT_LINK } from '@udecode/plate-link';
import {
  ELEMENT_LI,
  ELEMENT_OL,
  ELEMENT_TODO_LI,
  ELEMENT_UL,
} from '@udecode/plate-list';
import { ELEMENT_IMAGE, ELEMENT_MEDIA_EMBED } from '@udecode/plate-media';
import { describe, expect, it } from 'vitest';

import * as persistedKeys from 'components/plate-plugin-keys';
import { createParagraphPlugin } from 'components/paragraph-plugin';

describe('persisted Plate plugin keys', () => {
  it('keeps element type strings compatible with stored rawBody JSON', () => {
    expect({
      blockquote: ELEMENT_BLOCKQUOTE,
      codeBlock: ELEMENT_CODE_BLOCK,
      codeLine: ELEMENT_CODE_LINE,
      codeSyntax: ELEMENT_CODE_SYNTAX,
      emojiInput: ELEMENT_EMOJI_INPUT,
      h1: ELEMENT_H1,
      h2: ELEMENT_H2,
      h3: ELEMENT_H3,
      h4: ELEMENT_H4,
      h5: ELEMENT_H5,
      h6: ELEMENT_H6,
      image: ELEMENT_IMAGE,
      link: ELEMENT_LINK,
      listItem: ELEMENT_LI,
      mediaEmbed: ELEMENT_MEDIA_EMBED,
      orderedList: ELEMENT_OL,
      paragraph: persistedKeys.ELEMENT_PARAGRAPH,
      todo: ELEMENT_TODO_LI,
      unorderedList: ELEMENT_UL,
    }).toEqual({
      blockquote: 'blockquote',
      codeBlock: 'code_block',
      codeLine: 'code_line',
      codeSyntax: 'code_syntax',
      emojiInput: 'emoji_input',
      h1: 'h1',
      h2: 'h2',
      h3: 'h3',
      h4: 'h4',
      h5: 'h5',
      h6: 'h6',
      image: 'img',
      link: 'a',
      listItem: 'li',
      mediaEmbed: 'media_embed',
      orderedList: 'ol',
      paragraph: 'p',
      todo: 'action_item',
      unorderedList: 'ul',
    });
  });

  it('keeps mark keys compatible with stored rawBody JSON', () => {
    expect([
      MARK_BOLD,
      MARK_CODE,
      MARK_ITALIC,
      MARK_STRIKETHROUGH,
      MARK_UNDERLINE,
    ]).toEqual(['bold', 'code', 'italic', 'strikethrough', 'underline']);
  });

  it('keeps the app-owned migration keys aligned with Plate 36', () => {
    expect(persistedKeys).toMatchObject({
      ELEMENT_BLOCKQUOTE,
      ELEMENT_CODE_BLOCK,
      ELEMENT_CODE_LINE,
      ELEMENT_CODE_SYNTAX,
      ELEMENT_EMOJI_INPUT,
      ELEMENT_DEFAULT,
      ELEMENT_H1,
      ELEMENT_H2,
      ELEMENT_H3,
      ELEMENT_H4,
      ELEMENT_H5,
      ELEMENT_H6,
      ELEMENT_HR,
      ELEMENT_IMAGE,
      ELEMENT_LI,
      ELEMENT_LINK,
      ELEMENT_MEDIA_EMBED,
      ELEMENT_OL,
      ELEMENT_PARAGRAPH: 'p',
      ELEMENT_TODO_LI,
      ELEMENT_UL,
      MARK_BOLD,
      MARK_CODE,
      MARK_HIGHLIGHT,
      MARK_ITALIC,
      MARK_STRIKETHROUGH,
      MARK_SUBSCRIPT,
      MARK_SUPERSCRIPT,
      MARK_UNDERLINE,
      KEY_LIST_STYLE_TYPE,
    });
    expect(persistedKeys.HEADING_KEYS).toEqual(KEYS_HEADING);
  });

  it('keeps the removed Plate 36 paragraph package behavior', () => {
    const plugin = createParagraphPlugin();

    expect(plugin).toMatchObject({
      deserializeHtml: {
        rules: [{ validNodeName: 'P' }],
      },
      isElement: true,
      key: 'p',
      options: {
        hotkey: ['mod+opt+0', 'mod+shift+0'],
      },
    });
    expect(plugin.deserializeHtml?.query?.(document.createElement('p'))).toBe(
      true
    );
  });
});
