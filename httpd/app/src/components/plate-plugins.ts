import { withProps } from 'components/cn';
import React from 'react';
import {
  BlockquotePlugin,
  BoldPlugin,
  CodePlugin,
  H1Plugin,
  H2Plugin,
  H3Plugin,
  H4Plugin,
  H5Plugin,
  H6Plugin,
  HighlightPlugin,
  ItalicPlugin,
  StrikethroughPlugin,
  SubscriptPlugin,
  SuperscriptPlugin,
  UnderlinePlugin,
} from '@platejs/basic-nodes/react';
import {
  LineHeightPlugin,
  TextAlignPlugin,
} from '@platejs/basic-styles/react';
import { CaptionPlugin } from '@platejs/caption/react';
import {
  CodeBlockPlugin,
  CodeLinePlugin,
  CodeSyntaxPlugin,
} from '@platejs/code-block/react';
import {
  ExitBreakPlugin,
  TrailingBlockPlugin,
} from 'platejs';
import {
  ParagraphPlugin,
  PlateLeaf,
} from 'platejs/react';
import { EmojiPlugin } from '@platejs/emoji/react';
import { IndentPlugin } from '@platejs/indent/react';
import { ListPlugin as IndentListPlugin } from '@platejs/list/react';
import { JuicePlugin } from '@platejs/juice';
import { LinkPlugin } from '@platejs/link/react';
import { TodoListPlugin } from '@platejs/list-classic/react';
import { ImagePlugin, MediaEmbedPlugin } from '@platejs/media/react';
import { TabbablePlugin } from '@platejs/tabbable/react';

import {
  MarkdownShortcutsPlugin,
  blockquoteInputRule,
  codeBlockInputRule,
  codeBlockTrailingFenceInputRule,
  headingInputRule,
  indentListInputRules,
} from 'components/autoformat-plugin';
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
  KEY_LIST_STYLE_TYPE,
} from 'components/plate-plugin-keys';
import { BlockquoteElement } from 'components/plate-ui/blockquote-element';
import { CodeBlockElement } from 'components/plate-ui/code-block-element';
import { CodeLeaf } from 'components/plate-ui/code-leaf';
import { CodeLineElement } from 'components/plate-ui/code-line-element';
import { CodeSyntaxLeaf } from 'components/plate-ui/code-syntax-leaf';
import { EmojiInputElement } from 'components/plate-ui/emoji-input-element';
import {
  H1Element,
  H2Element,
  H3Element,
  H4Element,
  H5Element,
  H6Element,
} from 'components/plate-ui/heading-element';
import { HighlightLeaf } from 'components/plate-ui/highlight-leaf';
import { ImageElement } from 'components/plate-ui/image-element';
import { LinkElement } from 'components/plate-ui/link-element';
import { LinkFloatingToolbar } from 'components/plate-ui/link-floating-toolbar';
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

const AlignPlugin = TextAlignPlugin.extend({ key: 'align' as 'textAlign' });

export const plugins = [
  ParagraphPlugin.withComponent(ParagraphElement),
  // Register each heading level by key (official basic-blocks-kit style);
  // override.components does not reach the composite HeadingPlugin's nested
  // sub-plugins, and nothing consumes the composite "heading" key.
  H1Plugin.withComponent(H1Element).extend({ inputRules: [headingInputRule()] }),
  H2Plugin.withComponent(H2Element).extend({ inputRules: [headingInputRule()] }),
  H3Plugin.withComponent(H3Element).extend({ inputRules: [headingInputRule()] }),
  H4Plugin.withComponent(H4Element).extend({ inputRules: [headingInputRule()] }),
  H5Plugin.withComponent(H5Element).extend({ inputRules: [headingInputRule()] }),
  H6Plugin.withComponent(H6Element).extend({ inputRules: [headingInputRule()] }),
  BlockquotePlugin.withComponent(BlockquoteElement).extend({
    inputRules: [blockquoteInputRule],
  }),
  CodeBlockPlugin.withComponent(CodeBlockElement)
    .configurePlugin(CodeLinePlugin, { node: { component: CodeLineElement } })
    .configurePlugin(CodeSyntaxPlugin, { node: { component: CodeSyntaxLeaf } })
    .extend({
      inputRules: [codeBlockInputRule, codeBlockTrailingFenceInputRule],
      rules: {
        break: { default: 'lineBreak', empty: 'reset' },
        delete: { start: 'reset' },
      },
    }),
  LinkPlugin.withComponent(LinkElement).extend({
    render: { afterEditable: () => React.createElement(LinkFloatingToolbar) },
  }),
  ImagePlugin.withComponent(ImageElement),
  MediaEmbedPlugin.withComponent(MediaEmbedElement),
  CaptionPlugin.extend({
    options: {
      query: { allow: [ELEMENT_IMAGE, ELEMENT_MEDIA_EMBED] },
    },
  }),
  TodoListPlugin.withComponent(TodoListElement).extend({
    rules: {
      break: { empty: 'reset' },
      delete: { start: 'reset' },
    },
  }),

  BoldPlugin.withComponent(withProps(PlateLeaf, { as: 'strong' })),
  ItalicPlugin.withComponent(withProps(PlateLeaf, { as: 'em' })),
  UnderlinePlugin.withComponent(withProps(PlateLeaf, { as: 'u' })),
  StrikethroughPlugin.withComponent(withProps(PlateLeaf, { as: 's' })),
  CodePlugin.withComponent(CodeLeaf),
  HighlightPlugin.withComponent(HighlightLeaf),

  AlignPlugin.extend({ inject: { targetPlugins: textBlockTypes.slice(0, 4) } }),
  IndentPlugin.extend({ inject: { targetPlugins: textBlockTypes } }),
  IndentListPlugin.extend({
    inject: { targetPlugins: textBlockTypes },
    inputRules: indentListInputRules,
  }),
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

  MarkdownShortcutsPlugin,
  EmojiPlugin.configurePlugin({ key: ELEMENT_EMOJI_INPUT }, {
    node: { component: EmojiInputElement },
  }),
  ExitBreakPlugin.extend({
    shortcuts: {
      insert: { keys: 'mod+enter' },
      insertBefore: { keys: 'mod+shift+enter' },
    },
  }),
  TabbablePlugin.extend(({ editor }) => ({
    options: {
      query: () => {
        if (editor.selection) {
          const block = editor.api.block();
          if (
            block &&
            editor.api.isStart(editor.selection.anchor, block[1])
          ) {
            return false;
          }
        }
        return !editor.api.some({
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
