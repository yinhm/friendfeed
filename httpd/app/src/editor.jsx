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

/** @param {Value} nodes */
const serializePlainText = nodes => nodes.map(n => Node.string(n)).join('\n')

/** @type {Value} */
const initialValueEmpty = [
    {
        id: '1',
        type: ELEMENT_PARAGRAPH,
        children: [{ text: '' }],
    },
];

/**
 * Slate requires every root child to be an element. Legacy rawBody can be a
 * bare text string (pre-JSON imports) or JSON without element wrappers, both
 * of which crash editor normalization; wrap non-element children in a
 * paragraph.
 * @param {unknown} value
 * @returns {Value}
 */
const toEditorValue = (value) => {
    if (!Array.isArray(value) || value.length === 0) {
        return initialValueEmpty;
    }
    return /** @type {Value} */ (value.map((node) => {
        if (node && typeof node === 'object' && typeof node.type === 'string') {
            return node;
        }
        const textNode = node && typeof node === 'object' ? node : { text: String(node ?? '') };
        return { type: ELEMENT_PARAGRAPH, children: [textNode] };
    }));
};

/** @param {OnPageEditorProps} params */
const OnPageEditor = (params) => {
    const editorRef = useRef(/** @type {PlateEditor | null} */ (null));
    const eid = params.id + "editor";
    const [, setEditorValue] = useState(/** @type {Value | null} */ (null));

    const initialValue = useMemo(() => {
        if (params.content) {
            try {
                return toEditorValue(JSON.parse(params.content));
            } catch (_error) {
                const tmpEditor = createPlateEditor({ plugins });
                return toEditorValue(deserializeHtml(tmpEditor, {
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
        setEditorValue(slateValue);
    };

    const onPostEntry = useCallback(async () => {
        if (!editorRef.current) {
            return;
        }

        const editor = editorRef.current;
        const plainText = serializePlainText(editor.children);
        if (plainText.length < 8) {
            return;
        }
        const rawBody = JSON.stringify(editor.children)
        const htmlBody = await serializeEditorHtml(editor);

        const formData = new FormData();
        if (params.id) {
            formData.set("id", params.id);
        }
        formData.set("feedUuid", params.feedUuid);
        formData.set("body", htmlBody);
        formData.set("rawBody", rawBody);
        params.postEntry(formData)
            .then(() => editor.tf.reset())
            .catch((/** @type {unknown} */ error) => console.error(error));
    }, [params])

    return (
        <TooltipProvider
            disableHoverableContent
            delayDuration={500}
            skipDelayDuration={0}
        >
            <Plate
                editor={editor}
                onChange={({ value }) => onChange(value)}
            >
                <div className="sharebox">
                    <Editor
                        className="mb-4 min-h-[60px] cursor-text rounded-[2px] border-[#ddd] p-[15px]"
                        autoFocus={Boolean(params.id)}
                        focusRing={false}
                        variant="outline"
                        size="md"
                    />

                    <FloatingToolbar>
                      <FloatingToolbarButtons />
                    </FloatingToolbar>

                    <div className="post">
                        <button className="submit" type="button" onClick={onPostEntry}>发布</button>
                    </div>
                </div>
            </Plate>
        </TooltipProvider>
    );
}

export default OnPageEditor;
