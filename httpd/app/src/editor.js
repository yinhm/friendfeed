'use client';

import React, { useState, useEffect, useMemo, useRef } from 'react';
import { ReactEditor } from 'slate-react';
import { Node, Transforms } from 'slate'
import { TooltipProvider } from 'components/plate-ui/tooltip';
import {
    Plate,
    //   createDeserializeHTMLPlugin,
    serializeHtml,
    deserializeHtml,
    //   usePlateStates,
    //   useEditorRef,
    //   useReplaceEditor,
    //   useRef,
} from '@udecode/plate';

// import { Plate } from '@udecode/plate-common';
import { ELEMENT_PARAGRAPH } from '@udecode/plate-paragraph';
import { DndProvider } from 'react-dnd';
import { HTML5Backend } from 'react-dnd-html5-backend';

import { plugins } from 'components/plate-plugins';
import { Editor } from 'components/plate-ui/editor';
// import { FixedToolbar } from '@/components/plate-ui/fixed-toolbar';
// import { FixedToolbarButtons } from '@/components/plate-ui/fixed-toolbar-buttons';
// import { FloatingToolbar } from '@/components/plate-ui/floating-toolbar';
// import { FloatingToolbarButtons } from '@/components/plate-ui/floating-toolbar-buttons';


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

const initialValueEmpty = [
    {
        //   id: '1',
        type: ELEMENT_PARAGRAPH,
        children: [{ text: '' }],
    },
];

const OnPageEditor = ({
    id = "",
    feedUuid = "",
    content = "",
    postEntry
}) => {
    const editorRef = useRef(null);

    const eid = id + "editor";
    // const editorRef = useEditorRef(eid);
    const [focused, setFocused] = useState(false);
    const [editorValue, setEditorValue] = useState(null);
    // const { setValue, resetEditor } = usePlateActions(eid);
    // const [value, setValue] = usePlateStates('myeditor').value();

    // const [setValue, resetEditor] = usePlateStates(eid).value();


    // const plugins = useMemo(() => {
    //     const p = [...defaultPlugins];
    //     // p.push(createDeserializeHTMLPlugin({ plugins: p }));
    //     return p;
    // }, []);

    const initialValue = useMemo(() => {
        if (content) {
            try {
                // how to test content is raw?
                return JSON.parse(content);
            } catch (e) {
                // fail safe to html parse
                return deserializeHtml(ReactEditor, {
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
        //   const html = serializeHtml(editor, {
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

        const htmlBody = serializeHtml(ReactEditor, {
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
                // resetEditor(eid);
                editorRef.resetEditor();
                // setValue(initialValueEmpty);
            }).catch(error => console.error(error));
    };

    return (
        <TooltipProvider
            disableHoverableContent
            delayDuration={500}
            skipDelayDuration={0}
        >
            <DndProvider backend={HTML5Backend}>
                <Plate
                    id={eid}
                    plugins={plugins}
                    // components={components}
                    // options={options}
                    editableProps={editableProps}
                    initialValue={initialValue}
                    onChange={(newValue) => {
                        onChange(newValue);
                    }}
                >
                    <div className="sharebox" ref={editorRef}>

                        <Editor
                            className="px-[96px] py-16"
                            autoFocus
                            focusRing={false}
                            variant="ghost"
                            size="md"
                        />

                        <div className="post">
                            <span className="max_info"></span>
                            <input className="submit" type="submit" value="发布" onClick={onPostEntry} />
                        </div>
                    </div>
                </Plate>
            </DndProvider>
        </TooltipProvider>
    );
}

export default OnPageEditor;

