import React from 'react';
import { postJSON, postForm } from './utils';
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

import { withProps } from '@udecode/cn';
import {
    // createPlateUI,
    // createPlateOptions,
    // createEditorPlugins,
    createReactPlugin,
    createHistoryPlugin,
    // createBasicElementPlugins,
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
    createSoftBreakPlugin,
    createExitBreakPlugin,
    createTrailingBlockPlugin,
    KEYS_HEADING,
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
    getPluginType,
    MARK_BOLD,
    MARK_CODE,
    MARK_HIGHLIGHT,
    MARK_ITALIC,
    MARK_UNDERLINE,
    // BlockToolbarButton,
    // ToolbarLink,
    // ToolbarList,
    // MarkToolbarButton,
    usePlateEventId,
    // StyledElement,
    isBlockAboveEmpty,
    isSelectionAtBlockStart,
} from '@udecode/plate';
import { createPlateUI } from '@/plate/create-plate-ui';


import {
    CodeBlockToolbarButton,
    MarkToolbarButton,
} from '@udecode/plate-ui';

import { BalloonToolbar, BlockToolbarButton } from '@udecode/plate-toolbar';
import { LinkToolbarButton } from '@/components/LinkToolbarButton';
import { ListToolbarButton } from '@/components/ToolbarButton';

import {
    useEditorRef,
    useEditorSelector,
    useEventEditorSelectors,
    usePlateSelectors,
  } from '@udecode/plate-common';
import { css } from 'styled-components';


export const components = createPlateUI({
    // [ELEMENT_CODE_BLOCK]: withProps(CodeBlockElement, {
    //     styles: {
    //         root: [
    //             css`
    //         background-color: #111827;
    //         code {
    //           color: white;
    //         }
    //       `,
    //         ],
    //     },
    // }),
    // [ELEMENT_H1]: withProps(StyledElement, {
    //     styles: {
    //         root: {
    //             fontSize: "2em",
    //             marginBlock: "0.67em",
    //             marginInline: "0px",
    //             fontWeight: "bold",
    //         },
    //     },
    // }),
    // [ELEMENT_H2]: withProps(StyledElement, {
    //     styles: {
    //         root: {
    //             fontSize: "1.5em",
    //             marginBlock: "0.83em",
    //             marginInline: "0px",
    //             fontWeight: "bold",
    //         },
    //     },
    // }),
    // [ELEMENT_H3]: withProps(StyledElement, {
    //     styles: {
    //         root: {
    //             fontSize: "1.17em",
    //             marginBlock: "1em",
    //             marginInline: "0px",
    //             fontWeight: "bold",
    //         },
    //     },
    // }),
});

export const initialValueEmpty = [
    {
        type: ELEMENT_PARAGRAPH,
        children: [{ text: '' }]
    },
];

export const editor = createEditorPlugins();

export const options = createPlateOptions({});

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

const optionsSoftBreakPlugin = {
    rules: [
        { hotkey: 'shift+enter' },
        {
            hotkey: 'enter',
            query: {
                allow: [ELEMENT_CODE_BLOCK, ELEMENT_BLOCKQUOTE],
            },
        },
    ],
};

const optionsExitBreakPlugin = {
    rules: [
        {
            hotkey: 'mod+enter',
        },
        {
            hotkey: 'mod+shift+enter',
            before: true,
        },
        {
            hotkey: 'enter',
            query: {
                start: true,
                end: true,
                allow: KEYS_HEADING,
            },
        },
    ],
};

var hasArrayBufferView = new Blob([new Uint8Array(100)]).size == 100;

function dataURItoBlob(uri) {
    var data = uri.split(',')[1];
    var bytes = typeof atob === 'undefined' ? window.atob(data) : atob(data);
    var buf = new ArrayBuffer(bytes.length);
    var arr = new Uint8Array(buf);
    for (var i = 0; i < bytes.length; i++) {
        arr[i] = bytes.charCodeAt(i);
    }

    if (!hasArrayBufferView) arr = buf;
    var blob = new Blob([arr], { type: mime(uri) });
    blob.slice = blob.slice || blob.webkitSlice;
    return blob;
};

/**
 * Return data uri mime type.
 */

function mime(uri) {
    return uri.split(';')[0].slice(5);
}

function uploadImage(dataUrl) {
    var retUrl = "";
    var blobFile = dataURItoBlob(dataUrl);

    var formData = new FormData();
    formData.set("eid", ""); // no idea how to retrive entry
    formData.append("file", blobFile, "clipboard-file");

    return postForm("/a/upload", formData)
        .then(data => {
            retUrl = data.thumbUrl == "" ? data.url : data.thumbUrl;
            return retUrl;
        });
}

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
    createImagePlugin({ uploadImage: uploadImage }), // WithImageUploadOptions
    createSelectOnBackspacePlugin({ allow: [ELEMENT_IMAGE] }),

    // headers
    // ...createBasicElementPlugins(),

    // reset
    createResetNodePlugin(optionsResetBlockTypePlugin),
    createSoftBreakPlugin(optionsSoftBreakPlugin),
    createExitBreakPlugin(optionsExitBreakPlugin),
    createTrailingBlockPlugin({ type: ELEMENT_PARAGRAPH }),
    createDeserializeHTMLPlugin(),
];


export const InlineToolbarElements = () => {
    const editor = useEditorRef();

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
            <MarkToolbarButton
                type={getPluginType(editor, MARK_BOLD)}
                icon={<FormatBold />}
            />
            <MarkToolbarButton
                type={getPluginType(editor, MARK_ITALIC)}
                icon={<FormatItalic />}
            />
            <MarkToolbarButton
                type={getPluginType(editor, MARK_UNDERLINE)}
                icon={<FormatUnderlined />}
            />
            <MarkToolbarButton
                type={getPluginType(editor, MARK_CODE)}
                icon={<CodeAlt />}
            />
            <MarkToolbarButton
                type={getPluginType(editor, MARK_HIGHLIGHT)}
                icon={<Highlight />}
            />
            <BlockToolbarButton
                type={getPluginType(editor, ELEMENT_H1)}
                icon={<LooksOne />}
            />
            <BlockToolbarButton
                type={getPluginType(editor, ELEMENT_H2)}
                icon={<LooksTwo />}
            />
            <BlockToolbarButton
                type={getPluginType(editor, ELEMENT_H3)}
                icon={<Looks3 />}
            />
            <ListToolbarButton
                type={getPluginType(editor, ELEMENT_UL)}
                icon={<FormatListBulleted />}
            />
            <ListToolbarButton
                type={getPluginType(editor, ELEMENT_OL)}
                icon={<FormatListNumbered />}
            />
            <LinkToolbarButton icon={<Link />} />
            <BlockToolbarButton
                type={getPluginType(editor, ELEMENT_BLOCKQUOTE)}
                icon={<FormatQuote />}
            />
            <CodeBlockToolbarButton
                type={getPluginType(editor, ELEMENT_CODE_BLOCK)}
                icon={<CodeBlock />}
            />
        </BalloonToolbar>
    );
}
