// @ts-check

'use client';

import React, { useState, useMemo, useRef, useCallback } from 'react';
import { Node } from 'slate'
import { TooltipProvider } from 'components/plate-ui/tooltip';
import { deserializeHtml } from 'platejs';
import {
    createPlateEditor,
    Plate,
    usePlateEditor,
} from 'platejs/react';

import { ELEMENT_PARAGRAPH } from 'components/plate-plugin-keys';
import { plugins } from 'components/plate-plugins';
import { serializeEditorHtml } from 'components/plate-serialization';
import { Editor } from 'components/plate-ui/editor';
// import { FixedToolbar } from '@/components/plate-ui/fixed-toolbar';
// import { FixedToolbarButtons } from '@/components/plate-ui/fixed-toolbar-buttons';
import { FloatingToolbar } from 'components/plate-ui/floating-toolbar';
import { FloatingToolbarButtons } from 'components/plate-ui/floating-toolbar-buttons';

/** @typedef {import('platejs').Value} Value */
/** @typedef {import('platejs/react').PlateEditor} PlateEditor */

/**
 * @typedef {Object} OnPageEditorProps
 * @property {string=} id
 * @property {string} feedUuid
 * @property {string=} content
 * @property {(formData: FormData) => Promise<unknown>} postEntry
 */


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

/** @param {Value} nodes */
const serializePlainText = nodes => {
    return nodes.map(n => Node.string(n)).join('\n')
}

/** @type {Value} */
const initialValueEmpty = [
    {
        id: '1',
        type: ELEMENT_PARAGRAPH,
        children: [{ text: '' }],
    },
];

/** @param {OnPageEditorProps} params */
const OnPageEditor = (params) => {
    const editorRef = useRef(/** @type {PlateEditor | null} */ (null));

    const eid = params.id + "editor";
    // const editorRef = useEditorRef(eid);
    const [, setEditorValue] = useState(/** @type {Value | null} */ (null));
    // const { setValue, resetEditor } = usePlateActions(eid);
    // const [value, setValue] = usePlateStates('myeditor').value();
    // const [setValue, resetEditor] = usePlateStates(eid).value();

    // const plugins = useMemo(() => {
    //     const p = [...defaultPlugins];
    //     // p.push(createDeserializeHTMLPlugin({ plugins: p }));
    //     return p;
    // }, []);

    // https://github.com/udecode/plate/blob/main/apps/www/content/docs/accessing-editor.mdx#temporary-editor-instance
    const initialValue = useMemo(() => {
        // console.log("init value...")
        if (params.content) {
            try {
                // how to test content is raw?
                return /** @type {Value} */ (JSON.parse(params.content));
            } catch (_error) {
                // fail safe to html parse
                const tmpEditor = createPlateEditor({ plugins });
                return /** @type {Value} */ (deserializeHtml(tmpEditor, {
                    element: params.content,
                }));
            }
        }
        return initialValueEmpty;
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const editor = usePlateEditor(
        {
            autoSelect: params.id ? 'end' : false,
            id: eid,
            plugins,
            value: initialValue,
        },
        [eid, initialValue]
    );
    editorRef.current = editor;

    /** @param {Value} slateValue */
    const onChange = (slateValue) => {
        // console.log(JSON.stringify(slateValue));
        setEditorValue(slateValue);
    };

    const onPostEntry = useCallback(async () => {
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
        const htmlBody = await serializeEditorHtml(editor);

        var formData = new FormData();
        if (params.id) {
            formData.set("id", params.id);
        }
        formData.set("feedUuid", params.feedUuid);
        formData.set("body", htmlBody);
        formData.set("rawBody", rawBody);
        params.postEntry(formData)
            .then(() => {
                editor.tf.reset();
                // setValue(initialValueEmpty);
            }).catch((/** @type {unknown} */ error) => {
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
                editor={editor}
                onChange={({ value }) => {
                    onChange(value);
                }}
            >
                <div className="sharebox">

                    <Editor
                        className="px-[96px] py-16"
                        autoFocus={Boolean(params.id)}
                        focusRing={false}
                        variant="ghost"
                        size="md"
                        // placeholder='开始记录...'
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

                    <FloatingToolbar>
                      <FloatingToolbarButtons />
                    </FloatingToolbar>

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
