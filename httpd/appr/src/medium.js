import React, { useRef, useState, useEffect, createElement } from 'react';
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
    BalloonToolbar,
    CodeBlockElement,
    createBasicElementPlugins,
    createBlockquotePlugin,
    createBoldPlugin,
    createCodeBlockPlugin,
    createCodePlugin,
    createHeadingPlugin,
    createHighlightPlugin,
    createHistoryPlugin,
    createImagePlugin,
    createItalicPlugin,
    createLinkPlugin,
    createListPlugin,
    createParagraphPlugin,
    createReactPlugin,
    createSelectOnBackspacePlugin,
    createSlatePluginsComponents,
    createSlatePluginsOptions,
    createStrikethroughPlugin,
    createUnderlinePlugin,
    ELEMENT_BLOCKQUOTE,
    ELEMENT_CODE_BLOCK,
    ELEMENT_H1,
    ELEMENT_H2,
    ELEMENT_H3,
    ELEMENT_IMAGE,
    ELEMENT_OL,
    ELEMENT_UL,
    getSlatePluginType,
    MARK_BOLD,
    MARK_CODE,
    MARK_HIGHLIGHT,
    MARK_ITALIC,
    MARK_UNDERLINE,
    serializeHTMLFromNodes,
    deserializeHTMLToDocumentFragment,
    SlatePlugins,
    ToolbarCodeBlock,
    ToolbarElement,
    ToolbarLink,
    ToolbarList,
    ToolbarMark,
    useEventEditorId,
    useSlatePlugins,
    useStoreEditorRef,
    withProps,
} from '@udecode/slate-plugins';


import { css } from 'styled-components';


const plugins = [
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
    createStrikethroughPlugin(),  // strikethrough mark
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
];


const options = createSlatePluginsOptions();
const editableProps = {
    // placeholder: '请输入...',
    style: {
        padding: '15px',
    },
};
const components = createSlatePluginsComponents({
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


const OnPageEditor = (props) => {
    const [content, setContent] = useState("");
    const editor = useStoreEditorRef(useEventEditorId('focus'));

    useEffect(() => {
        // setContent("test content");
    }, []);

    const onChange = (value) => {
        setContent(value);
    };

    const postEntry = () => {
        const body = serializeHTMLFromNodes(editor, {
            plugins: plugins,
            nodes: editor.children,
        });
        console.log(body);
    };

    return (
        <div className="sharebox" id="shareform">
            <InlineToolbarElements />
            <SlatePlugins
                id="inline-editor"
                plugins={plugins}
                components={components}
                options={options}
                editableProps={editableProps}
                initialValue={content}
                onChange={(newValue) => {
                    onChange(newValue);
                }}
            />
            <div className="post">
                <span className="max_info"></span>
                <input className="submit" type="submit" value="发布" onClick={postEntry} />
            </div>
        </div>
    );
}

const InlineToolbarElements = () => {
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

export default OnPageEditor