import React, { Component } from 'react';
import Editor, { createEditorStateWithText } from '@draft-js-plugins/editor';
import { EditorState } from 'draft-js';

import createInlineToolbarPlugin, {
  Separator,
} from '@draft-js-plugins/inline-toolbar';
import {
  ItalicButton,
  BoldButton,
  UnderlineButton,
  CodeButton,
  HeadlineOneButton,
  HeadlineTwoButton,
  HeadlineThreeButton,
  UnorderedListButton,
  OrderedListButton,
  BlockquoteButton,
  CodeBlockButton,
} from '@draft-js-plugins/buttons';
import '@draft-js-plugins/inline-toolbar/lib/plugin.css';
import { convertToHTML, convertFromHTML } from 'draft-convert';


const inlineToolbarPlugin = createInlineToolbarPlugin();
const { InlineToolbar } = inlineToolbarPlugin;
const plugins = [inlineToolbarPlugin];

export default class InlineToolbarEditor extends Component {
  state = {
    editorState: EditorState.createEmpty(),
  };
 
  // constructor(props) {
  //   super(props);
  // }

  componentDidMount() {
    var content = this.props.content || "";
    var rawState = convertFromHTML(content);
    // fixing issue with SSR https://github.com/facebook/draft-js/issues/2332#issuecomment-761573306
    // eslint-disable-next-line react/no-did-mount-set-state
    this.setState({
      // editorState: createEditorStateWithText("<p>test</p>"),
      editorState: EditorState.createWithContent(rawState),
    });
  }

  onChange = (editorState) => {
    this.setState({
      editorState,
    });
    // var plainText = this.state.editorState.getPlainText("");
    // if (plainText == "" || plainText.length < 10) {
    //   // disable submit?
    // }
  };

  focus = () => {
    this.editor.focus();
  };

  postEntry = () => {
    var content = this.state.editorState.getCurrentContent();
    const htmlBody = convertToHTML(content);

    var plainText = content.getPlainText("");
    if (plainText === "" || plainText.length < 8) {
      return;
    }

    var formData = new FormData();
    formData.set("feedid", this.props.feedId || "");
    formData.set("body", htmlBody);
    this.props.postEntry(formData)
      .then(() => {
        this.setState({
          editorState: createEditorStateWithText(""),
        });
      }).catch(error => console.error(error));
  }

  render() {
    return (
      <div class="sharebox" id="shareform" onClick={this.focus}>
        <div className="editor" onClick={this.focus}>
          <Editor
            editorKey="InlineToolbarEditor"
            editorState={this.state.editorState}
            onChange={this.onChange}
            plugins={plugins}
            ref={(element) => {
              this.editor = element;
            }}
          />
          <InlineToolbar>
            {
              // may be use React.Fragment instead of div to improve perfomance after React 16
              (externalProps) => (
                <div>
                  <BoldButton {...externalProps} />
                  <ItalicButton {...externalProps} />
                  <UnderlineButton {...externalProps} />
                  <CodeButton {...externalProps} />
                  <Separator {...externalProps} />
                  <HeadlinesButton {...externalProps} />
                  <UnorderedListButton {...externalProps} />
                  <OrderedListButton {...externalProps} />
                  <BlockquoteButton {...externalProps} />
                  <CodeBlockButton {...externalProps} />
                </div>
              )
            }
          </InlineToolbar>
        </div>
        <div class="post">
          <span class="max_info"></span>
          <input class="submit" type="submit" value="发布" onClick={this.postEntry} />
        </div>
      </div>
    );
  }
}

class HeadlinesPicker extends Component {
  componentDidMount() {
    setTimeout(() => {
      window.addEventListener('click', this.onWindowClick);
    });
  }

  componentWillUnmount() {
    window.removeEventListener('click', this.onWindowClick);
  }

  onWindowClick = () =>
    // Call `onOverrideContent` again with `undefined`
    // so the toolbar can show its regular content again.
    this.props.onOverrideContent(undefined);

  render() {
    const buttons = [HeadlineOneButton, HeadlineTwoButton, HeadlineThreeButton];
    return (
      <div>
        {buttons.map((Button, i) => (
          // eslint-disable-next-line react/no-array-index-key
          <Button key={i} {...this.props} />
        ))}
      </div>
    );
  }
}

class HeadlinesButton extends Component {
  // When using a click event inside overridden content, mouse down
  // events needs to be prevented so the focus stays in the editor
  // and the toolbar remains visible  onMouseDown = (event) => event.preventDefault()
  onMouseDown = (event) => event.preventDefault();

  onClick = () =>
    // A button can call `onOverrideContent` to replace the content
    // of the toolbar. This can be useful for displaying sub
    // menus or requesting additional information from the user.
    this.props.onOverrideContent(HeadlinesPicker);

  render() {
    return (
      <div
        onMouseDown={this.onMouseDown}
        className="headlineButtonWrapper"
      >
        <button onClick={this.onClick} className="headlineButton">
          H
        </button>
      </div>
    );
  }
}
