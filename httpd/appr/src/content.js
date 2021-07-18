import React, { useContext } from 'react';
// import InlineToolbarEditor from './editor';
import OnPageEditor from './medium';
import { FeedContext } from './context'


export function EntryContent(props) {
    const feedCfg = useContext(FeedContext);

    const onPostEntry = (formData) => {
        var f = props.onPostEntry(formData);
        feedCfg.toggleEditor();
        return f;
    }

    if (props.onpage || feedCfg.onpage_edit === true) {
        return (
            <OnPageEditor
                id={props.id}
                feedId={feedCfg.feed_id}
                content={props.rawBody}
                postEntry={onPostEntry} />
        );
    }
    return (
        <div className="content" dangerouslySetInnerHTML={{ __html: props.body }}>
        </div>
    );
}