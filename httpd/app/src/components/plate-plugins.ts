import { withProps } from '@udecode/cn';
import React from 'react';
import { AlignPlugin } from '@udecode/plate-alignment/react';
import { AutoformatPlugin } from '@udecode/plate-autoformat/react';
import {
  BoldPlugin,
  CodePlugin,
  ItalicPlugin,
  StrikethroughPlugin,
  SubscriptPlugin,
  SuperscriptPlugin,
  UnderlinePlugin,
} from '@udecode/plate-basic-marks/react';
import { BlockquotePlugin } from '@udecode/plate-block-quote/react';
import { ExitBreakPlugin, SoftBreakPlugin } from '@udecode/plate-break/react';
import { CaptionPlugin } from '@udecode/plate-caption/react';
import {
  CodeBlockPlugin,
  CodeLinePlugin,
  CodeSyntaxPlugin,
} from '@udecode/plate-code-block/react';
import {
  isCodeBlockEmpty,
  isSelectionAtCodeBlockStart,
  unwrapCodeBlock,
} from '@udecode/plate-code-block';
import {
  isBlockAboveEmpty,
  isSelectionAtBlockStart,
  someNode,
} from '@udecode/plate-common';
import {
  ParagraphPlugin,
  PlateElement,
  PlateLeaf,
  toPlatePlugin,
} from '@udecode/plate-common/react';
import { EmojiPlugin } from '@udecode/plate-emoji/react';
import {
  FontBackgroundColorPlugin,
  FontColorPlugin,
  FontSizePlugin,
} from '@udecode/plate-font/react';
import { HeadingPlugin } from '@udecode/plate-heading/react';
import { HighlightPlugin } from '@udecode/plate-highlight/react';
import { IndentPlugin } from '@udecode/plate-indent/react';
import { IndentListPlugin } from '@udecode/plate-indent-list/react';
import { JuicePlugin } from '@udecode/plate-juice';
import { LineHeightPlugin } from '@udecode/plate-line-height/react';
import { LinkPlugin } from '@udecode/plate-link/react';
import { TodoListPlugin } from '@udecode/plate-list/react';
import { ImagePlugin, MediaEmbedPlugin } from '@udecode/plate-media/react';
import { NodeIdPlugin } from '@udecode/plate-node-id';
import { ResetNodePlugin } from '@udecode/plate-reset-node/react';
import { SelectOnBackspacePlugin } from '@udecode/plate-select';
import { TabbablePlugin } from '@udecode/plate-tabbable/react';
import { TrailingBlockPlugin } from '@udecode/plate-trailing-block';

import { autoformatPlugin } from 'components/autoformat-plugin';
import {
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
  ELEMENT_EMOJI_INPUT,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_IMAGE,
  ELEMENT_LI,
  ELEMENT_MEDIA_EMBED,
  ELEMENT_PARAGRAPH,
  ELEMENT_TODO_LI,
  HEADING_KEYS,
  KEY_LIST_STYLE_TYPE,
} from 'components/plate-plugin-keys';
import { BlockquoteElement } from 'components/plate-ui/blockquote-element';
import { CodeBlockElement } from 'components/plate-ui/code-block-element';
import { CodeLeaf } from 'components/plate-ui/code-leaf';
import { CodeLineElement } from 'components/plate-ui/code-line-element';
import { CodeSyntaxLeaf } from 'components/plate-ui/code-syntax-leaf';
import { EmojiInputElement } from 'components/plate-ui/emoji-input-element';
import { HeadingElement } from 'components/plate-ui/heading-element';
import { HighlightLeaf } from 'components/plate-ui/highlight-leaf';
import { ImageElement } from 'components/plate-ui/image-element';
import { LinkElement } from 'components/plate-ui/link-element';
import { LinkFloatingToolbar } from 'components/plate-ui/link-floating-toolbar';
import { ListElement } from 'components/plate-ui/list-element';
import { MediaEmbedElement } from 'components/plate-ui/media-embed-element';
import { ParagraphElement } from 'components/plate-ui/paragraph-element';
import { TodoListElement } from 'components/plate-ui/todo-list-element';

const textBlockTypes = [
  ELEMENT_PARAGRAPH,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
];

// Plate 37's React link plugin is already Plate-shaped at runtime, but its
// published type is not accepted by toPlatePlugin's Slate-only input type.
const AppLinkPlugin = toPlatePlugin(
  LinkPlugin as unknown as Parameters<typeof toPlatePlugin>[0]
);

export const plugins = [
  ParagraphPlugin.withComponent(ParagraphElement),
  HeadingPlugin.withComponent(HeadingElement),
  BlockquotePlugin.withComponent(BlockquoteElement),
  CodeBlockPlugin.withComponent(CodeBlockElement)
    .configurePlugin(CodeLinePlugin, { node: { component: CodeLineElement } })
    .configurePlugin(CodeSyntaxPlugin, { node: { component: CodeSyntaxLeaf } }),
  AppLinkPlugin.withComponent(LinkElement).extend({
    render: { afterEditable: () => React.createElement(LinkFloatingToolbar) },
  }),
  ImagePlugin.withComponent(ImageElement),
  MediaEmbedPlugin.withComponent(MediaEmbedElement),
  CaptionPlugin.extend({
    options: { plugins: [ImagePlugin, MediaEmbedPlugin] },
  }),
  TodoListPlugin.withComponent(TodoListElement),

  BoldPlugin.withComponent(withProps(PlateLeaf, { as: 'strong' })),
  ItalicPlugin.withComponent(withProps(PlateLeaf, { as: 'em' })),
  UnderlinePlugin.withComponent(withProps(PlateLeaf, { as: 'u' })),
  StrikethroughPlugin.withComponent(withProps(PlateLeaf, { as: 's' })),
  CodePlugin.withComponent(CodeLeaf),
  FontColorPlugin,
  FontBackgroundColorPlugin,
  FontSizePlugin,
  HighlightPlugin.withComponent(HighlightLeaf),

  AlignPlugin.extend({ inject: { targetPlugins: textBlockTypes.slice(0, 4) } }),
  IndentPlugin.extend({ inject: { targetPlugins: textBlockTypes } }),
  IndentListPlugin.extend({ inject: { targetPlugins: textBlockTypes } }),
  LineHeightPlugin.extend({
    inject: {
      nodeProps: {
        defaultNodeValue: 1.5,
        nodeKey: 'lineHeight',
        validNodeValues: [1, 1.2, 1.5, 2, 3],
      },
      targetPlugins: textBlockTypes.slice(0, 4),
    },
  }),

  AutoformatPlugin.extend({ options: autoformatPlugin }),
  EmojiPlugin.configurePlugin({ key: ELEMENT_EMOJI_INPUT }, {
    node: { component: EmojiInputElement },
  }),
  ExitBreakPlugin.extend({
    options: {
      rules: [
        { hotkey: 'mod+enter' },
        { hotkey: 'mod+shift+enter', before: true },
        {
          hotkey: 'enter',
          query: { start: true, end: true, allow: HEADING_KEYS },
          relative: true,
          level: 1,
        },
      ],
    },
  }),
  NodeIdPlugin,
  ResetNodePlugin.extend({
    options: {
      rules: [
        {
          types: [ELEMENT_BLOCKQUOTE, ELEMENT_TODO_LI],
          defaultType: ELEMENT_PARAGRAPH,
          hotkey: 'Enter',
          predicate: isBlockAboveEmpty,
        },
        {
          types: [ELEMENT_BLOCKQUOTE, ELEMENT_TODO_LI],
          defaultType: ELEMENT_PARAGRAPH,
          hotkey: 'Backspace',
          predicate: isSelectionAtBlockStart,
        },
        {
          types: [ELEMENT_CODE_BLOCK],
          defaultType: ELEMENT_PARAGRAPH,
          hotkey: 'Enter',
          predicate: isCodeBlockEmpty,
          onReset: unwrapCodeBlock,
        },
        {
          types: [ELEMENT_CODE_BLOCK],
          defaultType: ELEMENT_PARAGRAPH,
          hotkey: 'Backspace',
          predicate: isSelectionAtCodeBlockStart,
          onReset: unwrapCodeBlock,
        },
      ],
    },
  }),
  SelectOnBackspacePlugin.extend({
    options: { query: { allow: [ELEMENT_IMAGE] } },
  }),
  SoftBreakPlugin.extend({
    options: {
      rules: [
        { hotkey: 'shift+enter' },
        {
          hotkey: 'enter',
          query: { allow: [ELEMENT_CODE_BLOCK, ELEMENT_BLOCKQUOTE] },
        },
      ],
    },
  }),
  TabbablePlugin.extend(({ editor }) => ({
    options: {
      query: () => {
        if (isSelectionAtBlockStart(editor)) return false;
        return !someNode(editor, {
          match: (node) =>
            !!(
              node.type &&
              ([ELEMENT_LI, ELEMENT_CODE_BLOCK].includes(node.type as string) ||
                node[KEY_LIST_STYLE_TYPE])
            ),
        });
      },
    },
  })),
  TrailingBlockPlugin.extend({ options: { type: ELEMENT_PARAGRAPH } }),
  JuicePlugin,

  // Leaf renderers retained for old rawBody nodes even when their toolbar
  // actions are not currently exposed.
  SubscriptPlugin.withComponent(withProps(PlateLeaf, { as: 'sub' })),
  SuperscriptPlugin.withComponent(withProps(PlateLeaf, { as: 'sup' })),
];
