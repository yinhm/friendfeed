import React, { useContext, lazy, Suspense } from 'react';
import { FeedContext } from './context'

// Lazy-loaded: the Plate editor is a heavy chunk only needed when editing.
const OnPageEditor = lazy(() => import('./editor'));


export function EntryContent(props) {
    const feedCfg = useContext(FeedContext);

    const onPostEntry = (formData) => {
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
        <div className="content" dangerouslySetInnerHTML={{ __html: props.body }}>
        </div>
    );
}
