import React from 'react';
import { CodeAlt } from '@styled-icons/boxicons-regular/CodeAlt';
import { CodeBlock } from '@styled-icons/boxicons-regular/CodeBlock';
import { Highlight } from '@styled-icons/boxicons-regular/Highlight';
import { FormatBold } from '@styled-icons/material/FormatBold';
import { FormatItalic } from '@styled-icons/material/FormatItalic';
import { FormatListBulleted } from '@styled-icons/material/FormatListBulleted';
import { FormatListNumbered } from '@styled-icons/material/FormatListNumbered';
import { FormatQuote } from '@styled-icons/material/FormatQuote';
import { FormatUnderlined } from '@styled-icons/material/FormatUnderlined';
import { Link } from '@styled-icons/material/Link';
import { Looks3 } from '@styled-icons/material/Looks3';
import { LooksOne } from '@styled-icons/material/LooksOne';
import { LooksTwo } from '@styled-icons/material/LooksTwo';

import {
  createSlatePluginsComponents,
  createSlatePluginsOptions,
  createEditorPlugins,
  createReactPlugin,
  createHistoryPlugin,
  createBasicElementPlugins,
  createParagraphPlugin,
  createBlockquotePlugin,
  createHeadingPlugin,
  createImagePlugin,
  createLinkPlugin,
  createListPlugin,
  createCodeBlockPlugin,
  createBoldPlugin,
  createCodePlugin,
  createItalicPlugin,
  createHighlightPlugin,
  createSelectOnBackspacePlugin,
  createUnderlinePlugin,
  createResetNodePlugin,
  createTrailingBlockPlugin,
  BalloonToolbar,
  ELEMENT_BLOCKQUOTE,
  ELEMENT_CODE_BLOCK,
  ELEMENT_H1,
  ELEMENT_H2,
  ELEMENT_H3,
  ELEMENT_OL,
  ELEMENT_UL,
  ELEMENT_TODO_LI,
  ELEMENT_PARAGRAPH,
  ELEMENT_IMAGE,
  getSlatePluginType,
  MARK_BOLD,
  MARK_CODE,
  MARK_HIGHLIGHT,
  MARK_ITALIC,
  MARK_UNDERLINE,
  ToolbarCodeBlock,
  ToolbarElement,
  ToolbarLink,
  ToolbarList,
  ToolbarMark,
  useEventEditorId,
  useStoreEditorRef,
  withProps,
  CodeBlockElement,
  isBlockAboveEmpty,
  isSelectionAtBlockStart,
} from '@udecode/slate-plugins';
import { css } from 'styled-components';


export const components = createSlatePluginsComponents({
    [ELEMENT_CODE_BLOCK]: withProps(CodeBlockElement, {
        styles: {
            root: [
                css`
            background-color: #111827;
            code {
              color: white;
            }
          `,
            ],
        },
    }),
});

export const initialValueEmpty = [
    {
        children: [
            {
                type: 'paragraph',
                children: [{ text: '' }],
            },
        ],
    },
];

export const editor = createEditorPlugins();

export const options = createSlatePluginsOptions({});


const resetBlockTypesCommonRule = {
    types: [ELEMENT_BLOCKQUOTE, ELEMENT_TODO_LI],
    defaultType: ELEMENT_PARAGRAPH,
};

const optionsResetBlockTypePlugin = {
    rules: [
        {
            ...resetBlockTypesCommonRule,
            hotkey: 'Enter',
            predicate: isBlockAboveEmpty,
        },
        {
            ...resetBlockTypesCommonRule,
            hotkey: 'Backspace',
            predicate: isSelectionAtBlockStart,
        },
    ],
};


export const defaultPlugins = [
    // editor
    createReactPlugin(),          // withReact
    createHistoryPlugin(),        // withHistory

    // elements
    createParagraphPlugin(),      // paragraph element
    createBlockquotePlugin(),     // blockquote element
    createCodeBlockPlugin(),      // code block element
    createHeadingPlugin(),        // heading elements

    // marks
    createBoldPlugin(),           // bold mark
    createItalicPlugin(),         // italic mark
    createUnderlinePlugin(),      // underline mark
    createCodePlugin(),           // code mark

    // and more
    createHighlightPlugin(),
    createLinkPlugin(),
    createListPlugin(),

    // copy image from clipboard
    createImagePlugin(),
    createSelectOnBackspacePlugin({ allow: [ELEMENT_IMAGE] }),

    // headers
    ...createBasicElementPlugins(),

    // reset
    createResetNodePlugin(optionsResetBlockTypePlugin),
    // createSoftBreakPlugin(optionsSoftBreakPlugin),
    // createExitBreakPlugin(optionsExitBreakPlugin),
    createTrailingBlockPlugin({ type: ELEMENT_PARAGRAPH }),
];


export const InlineToolbarElements = () => {
    const editor = useStoreEditorRef(useEventEditorId('focus'));

    const arrow = false;
    const theme = 'dark';
    const direction = 'top';
    const hiddenDelay = 0;

    return (
        <BalloonToolbar
            direction={direction}
            hiddenDelay={hiddenDelay}
            theme={theme}
            arrow={arrow}
        >
            <ToolbarMark
                type={getSlatePluginType(editor, MARK_BOLD)}
                icon={<FormatBold />}
            />
            <ToolbarMark
                type={getSlatePluginType(editor, MARK_ITALIC)}
                icon={<FormatItalic />}
            />
            <ToolbarMark
                type={getSlatePluginType(editor, MARK_UNDERLINE)}
                icon={<FormatUnderlined />}
            />
            <ToolbarMark
                type={getSlatePluginType(editor, MARK_CODE)}
                icon={<CodeAlt />}
            />
            <ToolbarMark
                type={getSlatePluginType(editor, MARK_HIGHLIGHT)}
                icon={<Highlight />}
            />
            <ToolbarElement
                type={getSlatePluginType(editor, ELEMENT_H1)}
                icon={<LooksOne />}
            />
            <ToolbarElement
                type={getSlatePluginType(editor, ELEMENT_H2)}
                icon={<LooksTwo />}
            />
            <ToolbarElement
                type={getSlatePluginType(editor, ELEMENT_H3)}
                icon={<Looks3 />}
            />
            <ToolbarList
                type={getSlatePluginType(editor, ELEMENT_UL)}
                icon={<FormatListBulleted />}
            />
            <ToolbarList
                type={getSlatePluginType(editor, ELEMENT_OL)}
                icon={<FormatListNumbered />}
            />
            <ToolbarLink icon={<Link />} />
            <ToolbarElement
                type={getSlatePluginType(editor, ELEMENT_BLOCKQUOTE)}
                icon={<FormatQuote />}
            />
            <ToolbarCodeBlock
                type={getSlatePluginType(editor, ELEMENT_CODE_BLOCK)}
                icon={<CodeBlock />}
            />
        </BalloonToolbar>
    );
}
