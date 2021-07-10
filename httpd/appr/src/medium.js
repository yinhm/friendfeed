import React from 'react';
// import ReactDOM from 'react-dom';

import {
    Editor,
    createEditorState,
} from 'medium-draft';


export default class OnPageEditor extends React.Component {
    constructor(props) {
        super(props);
        this.state = {
            editorState: createEditorState(), // for empty content
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