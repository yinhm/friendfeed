import { describe, expect, it } from 'vitest';

import * as keys from 'components/plate-plugin-keys';

describe('persisted Plate plugin keys', () => {
  it('keeps element types compatible with stored rawBody JSON', () => {
    expect(keys).toMatchObject({
      ELEMENT_BLOCKQUOTE: 'blockquote',
      ELEMENT_CODE_BLOCK: 'code_block',
      ELEMENT_CODE_LINE: 'code_line',
      ELEMENT_CODE_SYNTAX: 'code_syntax',
      ELEMENT_DEFAULT: 'p',
      ELEMENT_EMOJI_INPUT: 'emoji_input',
      ELEMENT_H1: 'h1',
      ELEMENT_H2: 'h2',
      ELEMENT_H3: 'h3',
      ELEMENT_H4: 'h4',
      ELEMENT_H5: 'h5',
      ELEMENT_H6: 'h6',
      ELEMENT_HR: 'hr',
      ELEMENT_IMAGE: 'img',
      ELEMENT_LI: 'li',
      ELEMENT_LINK: 'a',
      ELEMENT_MEDIA_EMBED: 'media_embed',
      ELEMENT_OL: 'ol',
      ELEMENT_PARAGRAPH: 'p',
      ELEMENT_TODO_LI: 'action_item',
      ELEMENT_UL: 'ul',
      KEY_LIST_STYLE_TYPE: 'listStyleType',
    });
    expect(keys.HEADING_KEYS).toEqual(['h1', 'h2', 'h3', 'h4', 'h5', 'h6']);
  });

  it('keeps mark types compatible with stored rawBody JSON', () => {
    expect(keys).toMatchObject({
      MARK_BOLD: 'bold',
      MARK_CODE: 'code',
      MARK_HIGHLIGHT: 'highlight',
      MARK_ITALIC: 'italic',
      MARK_STRIKETHROUGH: 'strikethrough',
      MARK_SUBSCRIPT: 'subscript',
      MARK_SUPERSCRIPT: 'superscript',
      MARK_UNDERLINE: 'underline',
    });
  });
});
