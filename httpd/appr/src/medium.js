import React from 'react';
import { convertToRaw } from 'draft-js';

import {
    Editor,
    createEditorState,
} from 'medium-draft';
import mediumDraftImporter from 'medium-draft/lib/importer';


export default class OnPageEditor extends React.Component {
    constructor(props) {
        super(props);

        this.state = {
            editorState: createEditorState(convertToRaw(mediumDraftImporter(props.content))),
        };

        this.onChange = (editorState) => {
            this.setState({ editorState });
        };
    }

    componentDidMount() {
        this.refs.editor.focus();
    }

    render() {
        const { editorState } = this.state;
        return (
            <Editor
                ref="editor"
                editorState={editorState}
                onChange={this.onChange} />
        );
    }
};