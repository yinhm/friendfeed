import React, { useContext } from 'react';
import OnPageEditor from './editor';
import { FeedContext } from './context'
import { Tabular } from './table'


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

    if (props.type === "tabular") {
        if (feedCfg.onpage) {
            return (
                <Tabular rawBody={props.rawBody} />
            );
        }
        return (
            <div className="content">
                {props.title}
            </div>
        );
    }
    return (
        <div className="content" dangerouslySetInnerHTML={{ __html: props.body }}>
        </div>
    );
}