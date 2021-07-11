import React from 'react';
import OnPageEditor from './medium';
import InlineToolbarEditor from './editor';
import { convertToHTML, convertFromHTML } from 'draft-convert';


export function EntryContent(props) {

    const onPostEntry = () => {
        console.log("on-page post enry");
    }

    if (props.config.onpage_edit === true) {
        // return (
        //     <OnPageEditor 
        //         content={props.body} />
        // );
        
        return (
            <InlineToolbarEditor 
                feedId={props.config.feed_id}
                content={props.body}
                postEntry={onPostEntry} />
        );
    }
    return (
        <div className="content" dangerouslySetInnerHTML={{ __html: props.body }}>
        </div>
    );
}