import React, { useState } from 'react';
import { Entry } from './entry';
import { getJSON, postJSON, postForm } from './utils';
import OnPageEditor from './editor';
import { FeedContext } from './context'

function FeedPagin(props) {
  var prev = null;
  var next = null;
  var sep = null;
  var url = "?"
  if (props.query && props.query !== "") {
    url = '?q=' + props.query + '&';
  }
  if (props.show) {
    if (props.next > 30) {
      prev = <a href={url+'start='+props.prev}>&laquo; Prev</a>;
      sep = " ";
    }
    next = <a href={url+'start='+props.next}>Next &raquo;</a>;
  }
  return (
    <div className="pager bottom">
      {prev}{sep}{next}
    </div>
  );
}

function FeedHeader(props) {
  const [commands, setCommands] = useState(props.commands);

  const handleFollow = () => {
    var data = {
      feed_uuid: props.feedUuid,
      action: "follow"
    }
    postJSON("/a/follow", data)
      .then(data => { // arrow function
        setCommands(["unfollow"]);
      }).catch(error => console.error(error));
  };

  const handleUnfollow = () => {
    var data = {
      feed_uuid: props.feedUuid,
      action: "unfollow"
    }
    postJSON("/a/follow", data)
      .then(data => { // arrow function
        setCommands(["follow"]);
      }).catch(error => console.error(error));
  };

  var followBtn = "";
  if (commands) {
    var command = commands[0];
    if (command == "follow") {
      followBtn = (
        <a href="#nolink" onClick={handleFollow}>
          Follow
        </a>
      )
    }
    if (command == "unfollow") {
      followBtn = (
        <a href="#nolink" onClick={handleUnfollow}>
          Unfollow
        </a>
      )
    }
  }

  return (
    <div className="header">
      <div className="picture"><a href={"/feed/" + props.feedId}>
        <img src={props.picture} /></a>
      </div>
      <div className="body">
        <h1><a href={"/feed/" + props.feedId}>{props.name}</a></h1>

        <div className="description">{props.description}</div>

        {followBtn}
      </div>
      <div className="clear"></div>
    </div>
  )

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
        if (!new_state.feed.entries) {
          new_state.feed.entries = [];
        }
        new_state.feed.entries.unshift(data);
        this.setState(new_state);
      });
  }

  render() {
    if (!this.state.feed) {
      return null;
    }

    var config = this.context;
    config.show_header = this.state.show_header;
    config.show_paging = this.state.show_paging;
    config.show_share = this.state.show_share;
    config.onpage = this.state.onpage || false;
    config.onpage_edit = this.state.onpage_edit || false;
    config.feed_uuid = this.state.feed.uuid;
    config.toggleEditor = () => {
      console.log("toggle editor");
      config.onpage_edit = false;
    };

    var feed = this.state.feed;

    var feedHeader = "";
    if (this.state.show_header === true) {
      feedHeader = (
        <FeedHeader feedId={feed.id}
                    feedUuid={feed.uuid}
                    name={feed.name}
                    picture={feed.picture}
                    description={feed.description}
                    commands={feed.commands} />
      )
    }

    var entryNodes = ""
    if (feed.entries) {
      entryNodes = feed.entries.map((entry, index) => {
        return (
          <Entry entry={entry} key={entry.id} onpage_edit={this.state.onpage_edit}>
          </Entry>
        );
      });
    }

    var editorNodes = "";
    if (this.state.show_share === true) {
      editorNodes = (
        <OnPageEditor feedUuid={feed.uuid} postEntry={this.onPostEntry} />
      )
    }

    var feedPaginNodes = ""
    if (this.state.show_paging === true) {
      feedPaginNodes = (
        <FeedPagin show={this.state.show_paging} prev={this.state.prev_start}
                   next={this.state.next_start} query={this.state.query} />
      )
    }

    return (
      <FeedContext.Provider value={config}>
        {feedHeader}
        <div id="feed" className="feed">
          {editorNodes}
          {entryNodes}
          {feedPaginNodes}
        </div>
      </FeedContext.Provider>
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
  //     feedData: window.appData.feed
  //   });
  //   console.log(this.state.url);
  //   console.log(this.state.feedData);
  // }

  render() {
    var url = window.location.pathname + window.location.search;
    var appData = window.appData;
    var feedData = window.appData.feed;
    return (
      <Feed url={url} feed={feedData}
        show_header={appData.show_header}
        show_paging={appData.show_paging}
        show_share={appData.show_share}
        prev_start={appData.prev_start}
        next_start={appData.next_start}
        query={appData.query}
        onpage={appData.onpage}
        onpage_edit={appData.onpage_edit} />
    );
  }
}
