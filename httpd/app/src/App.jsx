// @ts-check

import React, { useState, lazy, Suspense } from 'react';
import { Entry } from './entry';
import { getJSON, postJSON, postForm } from './utils';
import { FeedContext } from './context'

// The Plate editor pulls in ~1.5 MB of slate/radix/plate code; load it
// on demand instead of making every reader download it.
const OnPageEditor = lazy(() => import('./editor'));

/**
 * @typedef {object} FeedEntry
 * @property {string} id
 * @property {{id: string, name: string, picture?: string, title?: string}} from
 * @property {string} body
 * @property {string[]} commands
 * @property {Record<string, unknown>} [data]
 *
 * @typedef {object} FeedData
 * @property {string} id
 * @property {string} uuid
 * @property {string} [name]
 * @property {string} [picture]
 * @property {string} [description]
 * @property {string[]} [commands]
 * @property {FeedEntry[]} [entries]
 *
 * @typedef {object} FeedProps
 * @property {string} url
 * @property {FeedData} feed
 * @property {boolean} show_header
 * @property {boolean} show_paging
 * @property {boolean} show_share
 * @property {number} prev_start
 * @property {number} next_start
 * @property {string} query
 * @property {boolean} onpage
 * @property {boolean} onpage_edit
 *
 * @typedef {FeedProps & {feed: FeedData}} FeedState
 *
 * @typedef {Omit<FeedProps, 'url'>} AppData
 */

/** @param {{query: string, show: boolean, prev: number, next: number}} props */
function FeedPagin(props) {
  /** @type {React.ReactNode} */
  var prev = null;
  /** @type {React.ReactNode} */
  var next = null;
  /** @type {React.ReactNode} */
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

/**
 * @param {{feedId: string, feedUuid: string, name?: string, picture?: string,
 * description?: string, commands?: string[]}} props
 */
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

  /** @type {React.ReactNode} */
  var followBtn = null;
  if (commands) {
    var command = commands[0];
    if (command === "follow") {
      followBtn = (
        <a href="#nolink" onClick={handleFollow}>
          Follow
        </a>
      )
    }
    if (command === "unfollow") {
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
        <img src={props.picture} alt="" /></a>
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

/** @extends {React.Component<FeedProps, FeedState>} */
export class Feed extends React.Component{
  static contextType  = FeedContext;

  refreshInterval = 20 * 1000

  /** @type {ReturnType<typeof setInterval> | undefined} */
  refreshFeed;

  loadFeeds = () => {
    getJSON(this.props.url)
      .then(data => { // arrow function
        this.setState(data);
      })
      .catch(error => console.error(error))
  }

  /** @param {FeedProps} props */
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

  /** @param {FormData} formData */
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

    var config = /** @type {React.ContextType<typeof FeedContext> & {onpage: boolean}} */ (
      this.context
    );
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

    /** @type {React.ReactNode} */
    var feedHeader = null;
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

    /** @type {React.ReactNode} */
    var entryNodes = null;
    if (feed.entries) {
      entryNodes = feed.entries.map((entry) => {
        return (
          <Entry entry={entry} key={entry.id} onpage_edit={this.state.onpage_edit}>
          </Entry>
        );
      });
    }

    /** @type {React.ReactNode} */
    var editorNodes = null;
    if (this.state.show_share === true) {
      editorNodes = (
        <Suspense fallback={<div className="editor-loading" role="status">Loading editor…</div>}>
          <OnPageEditor feedUuid={feed.uuid} postEntry={this.onPostEntry} />
        </Suspense>
      )
    }

    /** @type {React.ReactNode} */
    var feedPaginNodes = null;
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

/** @extends {React.Component<object, {url: string, feedData: object}>} */
export class App extends React.Component {

  /** @param {object} props */
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
    const appData = /** @type {Window & {appData: AppData}} */ (
      /** @type {unknown} */ (window)
    ).appData;
    var feedData = appData.feed;
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
