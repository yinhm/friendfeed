import React from 'react';
import OnPageEditor from './medium';


export function EntryContent(props) {
    if (props.config.onpage_edit === true) {
        return (
            <OnPageEditor 
                content={props.body} />
        );
    }
    return (
        <div className="content" dangerouslySetInnerHTML={{ __html: props.body }}>
        </div>
    );
}