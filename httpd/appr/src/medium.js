import React from 'react';
import { convertToRaw } from 'draft-js';
import { debounce } from 'throttle-debounce'

import {
    Editor,
    createEditorState,
} from 'medium-draft';
import mediumDraftImporter from 'medium-draft/lib/importer';
import mediumDraftExporter from 'medium-draft/lib/exporter';


export default class OnPageEditor extends React.Component {
    constructor(props) {
        super(props);

        this.state = {
            editorState: createEditorState(convertToRaw(mediumDraftImporter(props.content))),
        };

        this.editorRef = React.createRef();
    }

    componentDidMount() {
        this.editorRef.current.focus();
    }

    onChange = (editorState) => {
        this.setState({ editorState });
        this.updateEntry();
    }

    updateEntry = debounce(400, () => {
        var content = mediumDraftExporter(this.state.editorState.getCurrentContent());
        console.log(content);
    })

    render() {
        const { editorState } = this.state;
        return (
            <Editor
                ref={this.editorRef}
                editorState={editorState}
                onChange={this.onChange} />
        );
    }
};