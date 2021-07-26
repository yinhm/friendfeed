import React, { useState, useEffect, useMemo } from 'react';
import { ReactEditor } from 'slate-react';
import { Node, Editor, Transforms } from 'slate'
import {
  SlatePlugins,
  createDeserializeHTMLPlugin,
  serializeHTMLFromNodes,
  deserializeHTMLToDocumentFragment,
  useSlatePluginsActions,
  useStoreEditorRef,
} from '@udecode/slate-plugins';


import {
    InlineToolbarElements,
    initialValueEmpty,
    defaultPlugins,
    components,
    editor,
    options,
} from './options';

const editableProps = {
    placeholder: '开始记录...',
    style: {
        padding: '15px',
        boxSizing: "border-box",
        border: "1px solid #ddd",
        cursor: "text",
        borderRadius: "2px",
        marginBottom: "1em",
        minHeight: "60px",
    },
};

const serializePlainText = nodes => {
    return nodes.map(n => Node.string(n)).join('\n')
}


const OnPageEditor = ({
    id = "",
    feedUuid = "",
    content = "",
    postEntry
}) => {
    const eid = id + "editor";
    const editorRef = useStoreEditorRef(eid);
    const [focused, setFocused] = useState(false);
    const [editorValue, setEditorValue] = useState(null);
    const { setValue, resetEditor } = useSlatePluginsActions(eid);

    const plugins = useMemo(() => {
        const p = [...defaultPlugins];
        p.push(createDeserializeHTMLPlugin({ plugins: p }));
        return p;
    }, []);

    const initialValue = useMemo(() => {
        if (content) {
            try {
                // how to test content is raw?
                return JSON.parse(content);
            } catch (e) {
                // fail safe to html parse
                return deserializeHTMLToDocumentFragment(editor, {
                    plugins,
                    element: content,
                });
            }
        }
        return initialValueEmpty;
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [content]);

    // TODO:
    // automatic save
    useEffect(() => {
        if (editorRef && !focused && id !== "") {
            ReactEditor.focus(editorRef);
            Transforms.select(editorRef, Editor.end(editorRef, []));
            setFocused(true);
        }
        // if (editorValue) {
        //   const html = serializeHTMLFromNodes(editor, {
        //     plugins,
        //     nodes: editorValue,
        //   });
        //   console.log(html);
        // }
        // eslint-disable-next-line react-hooks/exhaustive-deps
      }, [editorRef, editorValue]);

    const onChange = (slateValue) => {
        // console.log(JSON.stringify(slateValue));
        setEditorValue(slateValue);
    };

    const onPostEntry = () => {
        if (!editorValue) {
            return
        }
        
        const htmlBody = serializeHTMLFromNodes(editor, {
            plugins,
            nodes: editorValue,
        });
        const rawBody = JSON.stringify(editorValue)

        var plainText = serializePlainText(editorValue);
        if (plainText.length < 8) {
            return;
        }

        var formData = new FormData();
        formData.set("id", id);
        formData.set("feedUuid", feedUuid);
        formData.set("body", htmlBody);
        formData.set("rawBody", rawBody);
        postEntry(formData)
            .then(() => {
                resetEditor(eid);
                setValue(initialValueEmpty);
            }).catch(error => console.error(error));
    };

    return (
        <div className="sharebox">
            <InlineToolbarElements />
            <SlatePlugins
                id={eid}
                plugins={plugins}
                components={components}
                options={options}
                editableProps={editableProps}
                initialValue={initialValue}
                onChange={(newValue) => {
                    onChange(newValue);
                }}
            />
            <div className="post">
                <span className="max_info"></span>
                <input className="submit" type="submit" value="发布" onClick={onPostEntry} />
            </div>
        </div>
    );
}

export default OnPageEditor
