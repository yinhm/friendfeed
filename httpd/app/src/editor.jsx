// @ts-check

'use client';

import React, { useState, useMemo, useRef, useCallback, useEffect } from 'react';
import { Node } from 'slate'
import { TooltipProvider } from 'components/plate-ui/tooltip';
import { deserializeHtml } from 'platejs';
import {
    createPlateEditor,
    Plate,
    usePlateEditor,
} from 'platejs/react';

import { ELEMENT_PARAGRAPH } from 'components/plate-plugin-keys';
import { ELEMENT_IMAGE } from 'components/plate-plugin-keys';
import { plugins } from 'components/plate-plugins';
import { serializeEditorHtml } from 'components/plate-serialization';
import { Editor } from 'components/plate-ui/editor';
import { FloatingToolbar } from 'components/plate-ui/floating-toolbar';
import { FloatingToolbarButtons } from 'components/plate-ui/floating-toolbar-buttons';
import {
    enrichImageNodes,
    mapWithConcurrency,
    MEDIA_UPLOAD_CONCURRENCY,
    mirrorPastedHTML,
    uploadAttachment,
    uploadImage,
} from './media-upload';
import { formatFileSize } from './entry-files';
import {FileUp, ImageUp} from 'lucide-react';

/** @typedef {import('platejs').Value} Value */
/** @typedef {import('platejs/react').PlateEditor} PlateEditor */

/**
 * @typedef {Object} OnPageEditorProps
 * @property {string=} id
 * @property {string} feedUuid
 * @property {string=} content
 * @property {{url: string, name: string, type?: string, size?: number}[]=} files
 * @property {'list' | 'permalink'=} responseMode
 * @property {(formData: FormData) => Promise<unknown>} postEntry
 */

/** @param {Value} nodes */
const serializePlainText = nodes => nodes.map(n => Node.string(n)).join('\n')

/** @param {any} node */
const containsImage = node => node?.type === ELEMENT_IMAGE || (node?.children ?? []).some(containsImage);

/** @param {any} node @param {string[]} tokens */
const collectImageAssetTokens = (node, tokens) => {
    if (node?.type === ELEMENT_IMAGE && typeof node.assetToken === 'string') tokens.push(node.assetToken);
    for (const child of node?.children ?? []) collectImageAssetTokens(child, tokens);
};

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
    const [pendingUploads, setPendingUploads] = useState(0);
    const [uploadError, setUploadError] = useState('');
    const [pasteWarning, setPasteWarning] = useState('');
    const pasteWarningTimer = useRef(/** @type {ReturnType<typeof setTimeout> | null} */ (null));
    const [attachments, setAttachments] = useState(/** @returns {Array<{url?: string, assetToken?: string, name: string, type?: string, mimeType?: string, size?: number, existing: boolean}>} */ () =>
        (params.files ?? []).map(file => ({...file, existing: true}))
    );

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

    const runUpload = useCallback(async (/** @type {() => Promise<void>} */ operation) => {
        setUploadError('');
        setPendingUploads(count => count + 1);
        try {
            await operation();
        } catch (error) {
            setUploadError(error instanceof Error ? error.message : 'Upload failed');
        } finally {
            setPendingUploads(count => count - 1);
        }
    }, []);

    const showPasteWarning = useCallback((/** @type {number} */ failures) => {
        if (pasteWarningTimer.current) clearTimeout(pasteWarningTimer.current);
        const noun = failures === 1 ? 'image was' : 'images were';
        setPasteWarning(`${failures} pasted ${noun} skipped because upload failed`);
        pasteWarningTimer.current = setTimeout(() => setPasteWarning(''), 6000);
    }, []);

    useEffect(() => () => {
        if (pasteWarningTimer.current) clearTimeout(pasteWarningTimer.current);
    }, []);

    const insertImageFiles = useCallback((/** @type {File[]} */ files) => runUpload(async () => {
        const results = await mapWithConcurrency(files, MEDIA_UPLOAD_CONCURRENCY,
            file => uploadImage(file));
        for (const image of results) {
            editor.tf.insertNodes({
                type: ELEMENT_IMAGE,
                url: image.url,
                originalUrl: image.originalUrl,
                assetToken: image.assetToken,
                width: image.width,
                height: image.height,
                children: [{text: ''}],
            });
        }
    }), [editor, runUpload]);

    const insertAttachmentFiles = useCallback((/** @type {File[]} */ files) => runUpload(async () => {
        if (attachments.length + files.length > 10) {
            throw new Error('An entry may contain at most 10 attachments');
        }
        const uploaded = await mapWithConcurrency(files, MEDIA_UPLOAD_CONCURRENCY,
            file => uploadAttachment(file));
        setAttachments(current => [...current, ...uploaded.map(file => ({...file, existing: false}))]);
    }), [attachments.length, runUpload]);

    const onPaste = useCallback((/** @type {React.ClipboardEvent<HTMLElement>} */ event) => {
        const files = Array.from(event.clipboardData.files ?? []);
        if (files.length > 0) {
            event.preventDefault();
            const images = files.filter(file => file.type.startsWith('image/'));
            const other = files.filter(file => !file.type.startsWith('image/'));
            if (images.length) insertImageFiles(images);
            if (other.length) insertAttachmentFiles(other);
            return;
        }
        // Plate owns its internal fragment format. Let its default paste
        // handler preserve existing canonical image nodes without uploading
        // them again through the HTML clipboard fallback.
        if (event.clipboardData.getData('application/x-slate-fragment')) return;
        const html = event.clipboardData.getData('text/html');
        if (!html || !/<img\b/i.test(html)) return;
        event.preventDefault();
        runUpload(async () => {
            const mirrored = await mirrorPastedHTML(html);
            const fragment = deserializeHtml(editor, {element: mirrored.html});
            enrichImageNodes(fragment, mirrored.metadata);
            editor.tf.insertFragment(fragment);
            if (mirrored.failures > 0) {
                showPasteWarning(mirrored.failures);
            }
        });
    }, [editor, insertAttachmentFiles, insertImageFiles, runUpload, showPasteWarning]);

    const onPostEntry = useCallback(async () => {
        if (!editorRef.current) {
            return;
        }

        const editor = editorRef.current;
        const plainText = serializePlainText(editor.children);
        if (plainText.length < 8 && !editor.children.some(containsImage) && attachments.length === 0) {
            return;
        }
        const rawBody = JSON.stringify(editor.children)
        const htmlBody = await serializeEditorHtml(editor);

        const formData = new FormData();
        if (params.id) {
            formData.set("id", params.id);
        }
        formData.set("feedUuid", params.feedUuid);
        formData.set("responseMode", params.responseMode === 'permalink' ? 'permalink' : 'list');
        formData.set("body", htmlBody);
        formData.set("rawBody", rawBody);
        formData.set("filesPresent", "1");
        const assets = /** @type {string[]} */ ([]);
        for (const node of editor.children) collectImageAssetTokens(node, assets);
        for (const file of attachments) {
            if (file.existing && file.url) formData.append("existingFile", file.url);
            else if (file.assetToken) assets.push(file.assetToken);
        }
        formData.set("assets", JSON.stringify(assets));
        params.postEntry(formData)
            .then(() => {
                editor.tf.reset();
                if (!params.id) setAttachments([]);
            })
            .catch((/** @type {unknown} */ error) => console.error(error));
    }, [attachments, params])

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
                        onPaste={onPaste}
                    />

                    <FloatingToolbar>
                      <FloatingToolbarButtons />
                    </FloatingToolbar>

                    {attachments.length > 0 && (
                      <div className="attachment-editor-list">
                        {attachments.map((file, index) => (
                          <div className="attachment-editor-item" key={`${file.url ?? file.name}-${index}`}>
                            <span>{file.name}</span>
                            <span className="entry-file-size">{formatFileSize(file.size)}</span>
                            <button type="button" className="inline-action"
                                    onClick={() => setAttachments(current => current.filter((_, i) => i !== index))}>
                              Remove
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                    {uploadError && (
                      <div className="upload-error" role="alert">
                        {uploadError}{' '}
                        <button type="button" className="inline-action" onClick={() => setUploadError('')}>Dismiss</button>
                      </div>
                    )}
                    <div className="post upload-actions">
                        {pasteWarning && <div className="paste-warning" role="status">{pasteWarning}</div>}
                        <label className="inline-action upload-action" aria-label="Add image" title="Add image">
                          <ImageUp size={18} aria-hidden="true" />
                          <input type="file" accept="image/jpeg,image/png,image/gif,image/webp" multiple hidden
                                 onChange={event => {
                                   const files = Array.from(event.target.files ?? []);
                                   if (files.length) insertImageFiles(files);
                                   event.target.value = '';
                                 }} />
                        </label>
                        <label className="inline-action upload-action" aria-label="Attach files" title="Attach files">
                          <FileUp size={18} aria-hidden="true" />
                          <input type="file" multiple hidden
                                 onChange={event => {
                                   const files = Array.from(event.target.files ?? []);
                                   if (files.length) insertAttachmentFiles(files);
                                   event.target.value = '';
                                 }} />
                        </label>
                        <button className="submit" type="button" onClick={onPostEntry}
                                disabled={pendingUploads > 0 || uploadError !== ''}>
                          {pendingUploads > 0 ? 'Uploading…' : '发布'}
                        </button>
                    </div>
                </div>
            </Plate>
        </TooltipProvider>
    );
}

export default OnPageEditor;
