import React, { useRef, useState, useEffect } from 'react';
import { EditorState } from 'draft-js';

import {
    Editor,
    // createEditorStateWithText
} from 'medium-draft';
import {stateFromHTML} from 'draft-js-import-html';
import {stateToHTML} from 'draft-js-export-html';


const options = {};


const OnPageEditor = (props) => {
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

    // updateEntry = debounce(400, () => {
    //     var content = stateToHTML(this.state.editorState.getCurrentContent());
    //     console.log(content);
    // })

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
                // setEditorState(createEditorStateWithText(""));
            }).catch(error => console.error(error));
    };

    return (
        <div className="sharebox" id="shareform" onClick={focus}>
            <div className="editor" onClick={focus}>
                <Editor
                    editorState={editorState}
                    onChange={onChange}
                    ref={(element) => {
                        editor.current = element;
                    }}
                />
            </div>
            <div className="post">
                <span className="max_info"></span>
                <input className="submit" type="submit" value="发布" onClick={postEntry} />
            </div>
        </div>
    );
};

export default OnPageEditor;
