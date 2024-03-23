'use client';

import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react';
import { ReactEditor } from 'slate-react';
import { Node } from 'slate'
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

import { plugins } from 'components/plate-plugins';
import { Editor } from 'components/plate-ui/editor';
// import { FixedToolbar } from '@/components/plate-ui/fixed-toolbar';
// import { FixedToolbarButtons } from '@/components/plate-ui/fixed-toolbar-buttons';
// import { FloatingToolbar } from '@/components/plate-ui/floating-toolbar';
// import { FloatingToolbarButtons } from '@/components/plate-ui/floating-toolbar-buttons';


// const editableProps = {
//     placeholder: '开始记录...',
//     style: {
//         padding: '15px',
//         boxSizing: "border-box",
//         border: "1px solid #ddd",
//         cursor: "text",
//         borderRadius: "2px",
//         marginBottom: "1em",
//         minHeight: "60px",
//     },
// };

const serializePlainText = nodes => {
    return nodes.map(n => Node.string(n)).join('\n')
}

const initialValueEmpty = [
    {
        id: '1',
        type: ELEMENT_PARAGRAPH,
        children: [{ text: 'From here...' }],
    },
];

const OnPageEditor = (params) => {
    const editorRef = useRef(null);

    const eid = params.id + "editor";
    // const editorRef = useEditorRef(eid);
    // const [focused, setFocused] = useState(false);
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
        if (params.content) {
            try {
                // how to test content is raw?
                return JSON.parse(params.content);
            } catch (e) {
                // fail safe to html parse
                return deserializeHtml(ReactEditor, {
                    plugins,
                    element: params.content,
                });
            }
        }
        return initialValueEmpty;
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [params]);

    // TODO:
    // automatic save
    useEffect(() => {
        console.log("useEffect: ", editorRef)
        // if (editorRef && !focused && params.id !== "") {
        //     ReactEditor.focus(editorRef);
        //     Transforms.select(editorRef, Editor.end(editorRef, []));
        //     setFocused(true);
        // }
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

    const onPostEntry = useCallback(() => {
        if (!editorRef || !editorRef.current) {
            console.log("no editor or content found");
            return
        }

        const editor = editorRef.current;
        var plainText = serializePlainText(editor.children);
        if (plainText.length < 8) {
            console.log("no valid content!");
            return;
        }
        const rawBody = JSON.stringify(editor.children)

        // see @udecode/plate/issues/2804
        const htmlBody = serializeHtml(editor, {
            nodes: editor.children,
        });

        var formData = new FormData();
        if (params.id) {
            formData.set("id", params.id);
        }
        formData.set("feedUuid", params.feedUuid);
        formData.set("body", htmlBody);
        formData.set("rawBody", rawBody);
        params.postEntry(formData)
            .then(() => {
                editor.reset();
                // setValue(initialValueEmpty);
            }).catch(error => {
                console.error(error)
            });
    }, [editorRef, params])

    return (
        <TooltipProvider
            disableHoverableContent
            delayDuration={500}
            skipDelayDuration={0}
        >
            <Plate
                id={eid}
                plugins={plugins}
                // components={components}
                // options={options}
                initialValue={initialValue}
                editorRef={editorRef}
                onChange={(newValue) => {
                    onChange(newValue);
                }}
            >
                <div className="sharebox">

                    <Editor
                        className="px-[96px] py-16"
                        autoFocus
                        focusRing={false}
                        variant="ghost"
                        size="md"
                        placeholder='开始记录...'
                        style={{
                            padding: '15px',
                            boxSizing: "border-box",
                            border: "1px solid #ddd",
                            cursor: "text",
                            borderRadius: "2px",
                            marginBottom: "1em",
                            minHeight: "60px",
                        }}
                    />

                    <div className="post">
                        <span className="max_info"></span>
                        <input className="submit" type="submit" value="发布" onClick={onPostEntry} />
                    </div>
                </div>
            </Plate>
        </TooltipProvider>
    );
}

export default OnPageEditor;

