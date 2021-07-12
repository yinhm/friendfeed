import React from 'react';
import OnPageEditor from './medium';
import InlineToolbarEditor from './editor';
import { convertToHTML, convertFromHTML } from 'draft-convert';


export function EntryContent(props) {

    const onPostEntry = () => {
        console.log("on-page post enry");
        // this.props.postEntry(props.body);
    }

    if (props.onpage || props.config.onpage_edit === true) {
        // return (
        //     <OnPageEditor 
        //         content={props.body} />
        // );
        
        return (
            <InlineToolbarEditor
                id={props.id}
                feedId={props.config.feed_id}
                content={props.body}
                postEntry={props.onPostEntry} />
        );
    }
    return (
        <div className="content" dangerouslySetInnerHTML={{ __html: props.body }}>
        </div>
    );
}