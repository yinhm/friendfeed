import React, { useContext } from 'react';
import OnPageEditor from './editor';
import { FeedContext } from './context'


export function EntryContent(props) {
    const feedCfg = useContext(FeedContext);

    const onPostEntry = (formData) => {
        var f = props.onPostEntry(formData);
        feedCfg.toggleEditor();
        return f;
    }

    if (props.onpageEdit || feedCfg.onpage_edit === true) {
        return (
            <OnPageEditor
                id={props.id}
                feedUuid={feedCfg.feed_uuid}
                content={props.rawBody}
                postEntry={onPostEntry} />
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