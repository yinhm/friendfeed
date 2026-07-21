// @ts-check

import React, { useContext, lazy, Suspense } from 'react';
import { FeedContext } from './context'
import { EntryBody } from './entry-body';

// Lazy-loaded: the Plate editor is a heavy chunk only needed when editing.
const OnPageEditor = lazy(() => import('./editor'));

/**
 * @typedef {object} EntryContentProps
 * @property {string} id
 * @property {string} [title]
 * @property {string} [rawBody]
 * @property {string} body
 * @property {string} [type]
 * @property {boolean} [onpageEdit]
 * @property {(formData: FormData) => Promise<any>} onPostEntry
 */

/** @param {EntryContentProps} props */
export function EntryContent(props) {
    const feedCfg = useContext(FeedContext);

    const onPostEntry = (/** @type {FormData} */ formData) => {
        var f = props.onPostEntry(formData);
        feedCfg.toggleEditor();
        return f;
    }

    if (props.onpageEdit || feedCfg.onpage_edit === true) {
        return (
            <Suspense fallback={<div className="editor-loading" role="status">Loading editor…</div>}>
                <OnPageEditor
                    id={props.id}
                    feedUuid={feedCfg.feed_uuid}
                    content={props.rawBody}
                    postEntry={onPostEntry} />
            </Suspense>
        );
    }

    // if (props.type === "tabular") {
    //     if (feedCfg.onpage) {
    //         const rawdata = JSON.parse(props.rawBody);

    //         return (
    //             <Tabular data={rawdata.data}
    //                 columns={rawdata.columns}
    //             />
    //         );
    //     }
    //     return (
    //         <div className="content">
    //             <a href={"/e/" + props.id}>{props.title}</a>
    //         </div>
    //     );
    // }
    return (
        <EntryBody rawBody={props.rawBody} body={props.body}
                   truncate={!feedCfg.onpage} entryId={props.id} />
    );
}
