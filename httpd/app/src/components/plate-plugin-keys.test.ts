import {
  MARK_BOLD,
  MARK_CODE,
  MARK_ITALIC,
  MARK_STRIKETHROUGH,
  MARK_UNDERLINE,
} from '@udecode/plate-basic-marks';
import { ELEMENT_BLOCKQUOTE } from '@udecode/plate-block-quote';
import {
  ELEMENT_CODE_BLOCK,
  ELEMENT_CODE_LINE,
  ELEMENT_CODE_SYNTAX,
} from '@udecode/plate-code-block';
import { ELEMENT_EMOJI_INPUT } from '@udecode/plate-emoji';
import { ELEMENT_H1, ELEMENT_H2, ELEMENT_H3 } from '@udecode/plate-heading';
import { ELEMENT_LINK } from '@udecode/plate-link';
import {
  ELEMENT_LI,
  ELEMENT_OL,
  ELEMENT_TODO_LI,
  ELEMENT_UL,
} from '@udecode/plate-list';
import { ELEMENT_IMAGE, ELEMENT_MEDIA_EMBED } from '@udecode/plate-media';
import { ELEMENT_PARAGRAPH } from '@udecode/plate-paragraph';
import { describe, expect, it } from 'vitest';

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
      image: ELEMENT_IMAGE,
      link: ELEMENT_LINK,
      listItem: ELEMENT_LI,
      mediaEmbed: ELEMENT_MEDIA_EMBED,
      orderedList: ELEMENT_OL,
      paragraph: ELEMENT_PARAGRAPH,
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
});
