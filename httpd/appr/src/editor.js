/* eslint-disable react/no-multi-comp */
import React, { useRef, useState, useEffect } from 'react';
import { EditorState } from 'draft-js';

import Editor, {
    createEditorStateWithText,
    composeDecorators
} from '@draft-js-plugins/editor';

import createInlineToolbarPlugin, {
    Separator,
} from '@draft-js-plugins/inline-toolbar';

import {
    ItalicButton,
    BoldButton,
    UnderlineButton,
    CodeButton,
    HeadlineOneButton,
    HeadlineTwoButton,
    HeadlineThreeButton,
    UnorderedListButton,
    OrderedListButton,
    BlockquoteButton,
    CodeBlockButton,
} from '@draft-js-plugins/buttons';
import createImagePlugin from '@draft-js-plugins/image';
import createFocusPlugin from '@draft-js-plugins/focus';
import createBlockDndPlugin from '@draft-js-plugins/drag-n-drop';
import createHashtagPlugin from '@draft-js-plugins/hashtag';
import createLinkifyPlugin from '@draft-js-plugins/linkify';
import '@draft-js-plugins/inline-toolbar/lib/plugin.css';
import '@draft-js-plugins/hashtag/lib/plugin.css';
// import editorStyles from './editorStyles.css';
import {stateFromHTML} from 'draft-js-import-html';
import {stateToHTML} from 'draft-js-export-html';


const inlineToolbarPlugin = createInlineToolbarPlugin();
const { InlineToolbar } = inlineToolbarPlugin;

const focusPlugin = createFocusPlugin();
const blockDndPlugin = createBlockDndPlugin();
const hashtagPlugin = createHashtagPlugin();
const linkifyPlugin = createLinkifyPlugin();
const decorator = composeDecorators(
    focusPlugin.decorator,
    blockDndPlugin.decorator
);
const imagePlugin = createImagePlugin({ decorator });

const plugins = [
    inlineToolbarPlugin,
    blockDndPlugin,
    focusPlugin,
    imagePlugin,
    linkifyPlugin,
    hashtagPlugin
];

let options = {
    // inlineStyles: {
    //     'code-block': { element: 'pre' },
    // },
};

const InlineToolbarEditor = (props) => {
    const [editorState, setEditorState] = useState(
        EditorState.createEmpty()
    );
    const editor = useRef();

    useEffect(() => {
        var content = props.content || "";
        var rawState = stateFromHTML(content);
        // fixing issue with SSR https://github.com/facebook/draft-js/issues/2332#issuecomment-761573306
        setEditorState(EditorState.createWithContent(rawState));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const onChange = (value) => {
        setEditorState(value);
    };

    const focus = () => {
        editor.current.focus();
    };

    const postEntry = () => {
        var content = editorState.getCurrentContent();
        const htmlBody = stateToHTML(content, options);

        var plainText = content.getPlainText("");
        if (plainText === "" || plainText.length < 8) {
            return;
        }

        var formData = new FormData();
        formData.set("id", props.id || "");
        formData.set("feedid", props.feedId || "");
        formData.set("body", htmlBody);
        props.postEntry(formData)
            .then(() => {
                setEditorState(createEditorStateWithText(""));
            }).catch(error => console.error(error));
    };

    return (
        <div className="sharebox" id="shareform" onClick={focus}>
            <div className="editor" onClick={focus}>
                <Editor
                    editorState={editorState}
                    onChange={onChange}
                    plugins={plugins}
                    ref={(element) => {
                        editor.current = element;
                    }}
                />
                <InlineToolbar>
                    {
                        // may be use React.Fragment instead of div to improve perfomance after React 16
                        (externalProps) => (
                            <div>
                                <BoldButton {...externalProps} />
                                <ItalicButton {...externalProps} />
                                <UnderlineButton {...externalProps} />
                                <CodeButton {...externalProps} />
                                <Separator {...externalProps} />
                                <HeadlinesButton {...externalProps} />
                                <UnorderedListButton {...externalProps} />
                                <OrderedListButton {...externalProps} />
                                <BlockquoteButton {...externalProps} />
                                <CodeBlockButton {...externalProps} />
                            </div>
                        )
                    }
                </InlineToolbar>
            </div>
            <div className="post">
                <span className="max_info"></span>
                <input className="submit" type="submit" value="发布" onClick={postEntry} />
            </div>
        </div>
    );
};

const HeadlinesPicker = (props) => {
    const onWindowClick = () =>
        // Call `onOverrideContent` again with `undefined`
        // so the toolbar can show its regular content again.
        props.onOverrideContent(undefined);

    useEffect(() => {
        const timeout = setTimeout(() => {
            window.addEventListener('click', onWindowClick);
        });

        return () => {
            if (timeout) {
                clearTimeout(timeout);
            }

            window.removeEventListener('click', onWindowClick);
        };
    });

    const buttons = [HeadlineOneButton, HeadlineTwoButton, HeadlineThreeButton];
    return (
        <div>
            {buttons.map((
                Button,
                i // eslint-disable-next-line
            ) => (
                // eslint-disable-next-line react/no-array-index-key
                <Button key={i} {...props} />
            ))}
        </div>
    );
};

const HeadlinesButton = ({ onOverrideContent }) => {
    // When using a click event inside overridden content, mouse down
    // events needs to be prevented so the focus stays in the editor
    // and the toolbar remains visible  onMouseDown = (event) => event.preventDefault()
    const onMouseDown = (event) => event.preventDefault();

    const onClick = () =>
        // A button can call `onOverrideContent` to replace the content
        // of the toolbar. This can be useful for displaying sub
        // menus or requesting additional information from the user.
        onOverrideContent(HeadlinesPicker);

    return (
        <div
            onMouseDown={onMouseDown}
            className="headlineButtonWrapper">
            <button onClick={onClick} className="headlineButton">
                H
            </button>
        </div>
    );
};

export default InlineToolbarEditor;