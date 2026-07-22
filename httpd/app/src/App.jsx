// @ts-check

import React, { useContext, useEffect, useRef, useState, lazy, Suspense } from 'react';
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
      .then(() => { // arrow function
        setCommands(["unfollow"]);
      }).catch(error => console.error(error));
  };

  const handleUnfollow = () => {
    var data = {
      feed_uuid: props.feedUuid,
      action: "unfollow"
    }
    postJSON("/a/follow", data)
      .then(() => { // arrow function
        setCommands(["follow"]);
      }).catch(error => console.error(error));
  };

  /** @type {React.ReactNode} */
  var followBtn = null;
  if (commands) {
    var command = commands[0];
    if (command === "follow") {
      followBtn = (
        <button type="button" className="inline-action header-action" onClick={handleFollow}>
          Follow
        </button>
      )
    }
    if (command === "unfollow") {
      followBtn = (
        <button type="button" className="inline-action header-action" onClick={handleUnfollow}>
          Unfollow
        </button>
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

/** @param {FeedProps} props */
export function Feed(props) {
  const [state, setState] = useState(/** @type {FeedState} */ ({...props}));
  const urlRef = useRef(props.url);
  urlRef.current = props.url;

  const context = useContext(FeedContext);

  // UNSAFE_componentWillReceiveProps(nextProps){
  //   dprint("componentWillReceiveProps");
  //   this.setState(this.getInitialState(nextProps));
  // }

  useEffect(() => {
    const refreshFeed = setInterval(() => {
      getJSON(urlRef.current)
        .then(data => {
          setState(current => ({...current, ...data}));
        })
        .catch(error => console.error(error));
    }, 20 * 1000);

    return () => clearInterval(refreshFeed);
  }, []);

  /** @param {FormData} formData */
  const onPostEntry = (formData) => {
    // on post
    return postForm("/a/share", formData)
      .then(data => {
        setState(current => ({
          ...current,
          feed: {
            ...current.feed,
            entries: [data, ...(current.feed.entries ?? [])],
          },
        }));
      });
  };

  if (!state.feed) {
    return null;
  }

  var config = /** @type {React.ContextType<typeof FeedContext> & {onpage: boolean}} */ (
    context
  );
  config.show_header = state.show_header;
  config.show_paging = state.show_paging;
  config.show_share = state.show_share;
  config.onpage = state.onpage || false;
  config.onpage_edit = state.onpage_edit || false;
  config.feed_uuid = state.feed.uuid;
  config.toggleEditor = () => {
    console.log("toggle editor");
    config.onpage_edit = false;
  };

  var feed = state.feed;

  /** @type {React.ReactNode} */
  var feedHeader = null;
  if (state.show_header === true) {
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
        <Entry entry={entry} key={entry.id} onpage_edit={state.onpage_edit}>
        </Entry>
      );
    });
  }

  /** @type {React.ReactNode} */
  var editorNodes = null;
  if (state.show_share === true) {
    editorNodes = (
      <Suspense fallback={<div className="editor-loading" role="status">Loading editor…</div>}>
        <OnPageEditor feedUuid={feed.uuid} postEntry={onPostEntry} />
      </Suspense>
    )
  }

  /** @type {React.ReactNode} */
  var feedPaginNodes = null;
  if (state.show_paging === true) {
    feedPaginNodes = (
      <FeedPagin show={state.show_paging} prev={state.prev_start}
                 next={state.next_start} query={state.query} />
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

export function App() {
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
