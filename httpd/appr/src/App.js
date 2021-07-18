import React from 'react';
import { Entry } from './entry';
import { getJSON, postForm } from './utils';
// import InlineToolbarEditor from './editor';
import OnPageEditor from './medium';
import { FeedContext } from './context'


function FeedPagin(props) {
  var prev = null;
  var next = null;
  var sep = null;
  if (props.show) {
    if (props.next > 30) {
      prev = <a href={'?start='+props.prev}>&laquo; Prev</a>;
      sep = " ";
    }
    next = <a href={'?start='+props.next}>Next &raquo;</a>;
  }
  return (
    <div className="pager bottom">
      {prev}{sep}{next}
    </div>
  );
}

export class Feed extends React.Component{
  static contextType  = FeedContext;

  refreshInterval = 20 * 1000

  loadFeeds = () => {
    getJSON(this.props.url)
      .then(data => { // arrow function
        this.setState(data);
      })
      .catch(error => console.error(error))
  }

  constructor(props) {
    super(props);
    // supress warnning:
    // It is not recommended to assign props directly to
    // state because updates to props won't be reflected
    // in state. In most cases, it is better to use props directly.
    this.state = {...props}
  }

  // UNSAFE_componentWillReceiveProps(nextProps){
  //   dprint("componentWillReceiveProps");
  //   this.setState(this.getInitialState(nextProps));
  // }

  componentDidMount() {
    this.refreshFeed = setInterval(
      () => this.loadFeeds(),
      this.refreshInterval
    );
  }

  componentWillUnmount() {
    clearInterval(this.refreshFeed);
  }

  onPostEntry = (formData) => {
    // on post
    return postForm("/a/share", formData)
      .then(data => {
        var new_state = Object.assign({}, this.state);
        new_state.feed.entries.unshift(data);
        this.setState(new_state);
      }).catch(error => console.error(error));
  }

  render() {
    if (!this.state.feed || !this.state.feed.entries) {
      return null;
    }

    var config = this.context;
    config.show_header = this.state.show_header;
    config.show_paging = this.state.show_paging;
    config.show_share = this.state.show_share;
    config.onpage_edit = this.state.onpage_edit || false;
    config.feed_id = this.state.feed.id;
    config.toggleEditor = () => {
      console.log("toggle editor");
      config.onpage_edit = false;
    };

    var feed = this.state.feed;
    var entryNodes = feed.entries.map((entry, index) => {
      return (
        <Entry entry={entry} key={entry.id} onpage_edit={this.state.onpage_edit}>
        </Entry>
      );
    });

    var editorNodes = "";
    if (this.state.show_share === true) {
      editorNodes = (
        <OnPageEditor feedId={feed.Id} postEntry={this.onPostEntry} />
      )
    }

    var feedPaginNodes = ""
    if (this.state.show_paging === true) {
      feedPaginNodes = (
        <FeedPagin show={this.state.show_paging} prev={this.state.prev_start}
                   next={this.state.next_start} />
      )
    }

    return (
      <div className="feed">
        <FeedContext.Provider value={config}>
          {editorNodes}
          {entryNodes}
          {feedPaginNodes}
        </FeedContext.Provider>
      </div>
    );
  }
}

export class App extends React.Component {

  constructor(props) {
    super(props);
    this.state = {url: "/", feedData:{}};
  }

  // NOT WORK??
  // componentDidMount() {
  //   console.log("app, componentDidMount")
  //   this.setState({
  //     url: window.location.pathname + window.location.search,
  //     feedData: window.app_props.feed
  //   });
  //   console.log(this.state.url);
  //   console.log(this.state.feedData);
  // }

  render() {
    var url = window.location.pathname + window.location.search;
    var appData = window.app_props;
    var feedData = window.app_props.feed;
    return (
      <Feed url={url} feed={feedData}
        show_header={appData.show_header}
        show_paging={appData.show_paging}
        show_share={appData.show_share}
        onpage_edit={appData.onpage_edit} />
    );
  }
}
