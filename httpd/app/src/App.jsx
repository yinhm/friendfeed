// @ts-check

import React, { useCallback, useContext, useEffect, useState, lazy, Suspense } from 'react';
import { Entry } from './entry';
import { getJSON, postJSON, postForm } from './utils';
import { FeedContext } from './context'

// The Plate editor pulls in ~1.5 MB of slate/radix/plate code; load it
// on demand instead of making every reader download it.
const OnPageEditor = lazy(() => import('./editor'));

/**
 * @typedef {import('./browser-types').EntryView} FeedEntry
 * @typedef {import('./browser-types').FeedView} FeedData
 *
 * @typedef {object} FeedProps
 * @property {string} url
 * @property {FeedData} feed
 * @property {boolean} show_header
 * @property {boolean} show_paging
 * @property {boolean} show_share
 * @property {number} prev_start
 * @property {number} next_start
 * @property {boolean} [show_prev]
 * @property {boolean} [show_next]
 * @property {boolean} [cursor_paging]
 * @property {string} [next_cursor]
 * @property {boolean} [realtime_enabled]
 * @property {boolean} [realtime_home]
 * @property {string} [manage_services_url]
 * @property {string} [group_settings_url]
 * @property {string} [group_members_url]
 * @property {string} query
 * @property {boolean} onpage
 * @property {boolean} onpage_edit
 *
 * @typedef {FeedProps & {feed: FeedData}} FeedState
 *
 * @typedef {Omit<FeedProps, 'url'>} AppData
 */

/** @param {{query: string, show: boolean, prev: number, next: number, showPrev?: boolean, showNext?: boolean, cursorPaging?: boolean, nextCursor?: string}} props */
function FeedPagin(props) {
  /** @type {React.ReactNode} */
  var prev = null;
  /** @type {React.ReactNode} */
  var next = null;
  /** @type {React.ReactNode} */
  var sep = null;
  var url = "?"
  if (props.query && props.query !== "") {
    url = '?q=' + encodeURIComponent(props.query) + '&';
  }
  if (props.show) {
    if (props.cursorPaging) {
      if (props.nextCursor) {
        next = <a href={'?cursor='+encodeURIComponent(props.nextCursor)}>Next &raquo;</a>;
      }
    } else {
      if (props.showPrev) {
        prev = <a href={url+'start='+props.prev}>&laquo; Prev</a>;
      }
      if (props.showPrev && props.showNext) {
        sep = " ";
      }
      if (props.showNext) {
        next = <a href={url+'start='+props.next}>Next &raquo;</a>;
      }
    }
  }
  return (
    <div className="pager bottom">
      {prev}{sep}{next}
    </div>
  );
}

/**
 * @param {{feedId: string, feedUuid: string, name?: string, picture?: string,
 * description?: string, private?: boolean, commands?: string[],
 * manageServicesUrl?: string, groupSettingsUrl?: string, groupMembersUrl?: string}} props
 */
function FeedHeader(props) {
  const [commands, setCommands] = useState(props.commands);
  const [followError, setFollowError] = useState('');

  const handleFollow = () => {
    var data = {
      feed_uuid: props.feedUuid,
      action: "follow"
    }
    postJSON("/a/follow", data)
      .then((resp) => {
        setFollowError('');
        // A private target turns the follow into a pending request.
        setCommands([resp && resp.requested ? "requested" : "unfollow"]);
      }).catch(error => setFollowError(error.message));
  };

  const handleUnfollow = () => {
    var data = {
      feed_uuid: props.feedUuid,
      action: "unfollow"
    }
    postJSON("/a/follow", data)
      .then(() => { // arrow function
        setFollowError('');
        setCommands(["follow"]);
      }).catch(error => setFollowError(error.message));
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
    if (command === "requested") {
      followBtn = (
        <button type="button" className="inline-action header-action" disabled>
          Requested
        </button>
      )
    }
  }

  return (
    <div className="header">
      <div className="picture"><a href={"/feed/" + props.feedId}>
        <img src={props.picture} alt={props.name ?? ''} /></a>
      </div>
      <div className="body">
        <h1>
          <a href={"/feed/" + props.feedId}>{props.name}</a>
          {props.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
        </h1>

        <div className="description">{props.description}</div>

        {followBtn}
        {followError && <div role="alert" className="error-banner">{followError}</div>}
      </div>
      <div className="clear"></div>
      <FeedManagementNav feedId={props.feedId}
                         manageServicesUrl={props.manageServicesUrl}
                         groupSettingsUrl={props.groupSettingsUrl}
                         groupMembersUrl={props.groupMembersUrl} />
    </div>
  )

}

/**
 * @param {{feedId: string, manageServicesUrl?: string,
 * groupSettingsUrl?: string, groupMembersUrl?: string}} props
 */
function FeedManagementNav(props) {
  if (!props.manageServicesUrl && !props.groupSettingsUrl && !props.groupMembersUrl) return null;
  const linkClass = 'border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground hover:text-foreground';
  return (
    <nav className="mb-6 flex gap-1 border-b border-border" aria-label="Feed management">
      <a href={'/feed/' + props.feedId} aria-current="page"
         className="border-b-2 border-primary px-3 py-2 text-sm font-medium text-primary">Feed</a>
      {props.groupSettingsUrl && <a href={props.groupSettingsUrl} className={linkClass}>Settings</a>}
      {props.groupMembersUrl && <a href={props.groupMembersUrl} className={linkClass}>Members</a>}
      {props.manageServicesUrl && <a href={props.manageServicesUrl} className={linkClass}>Import Services</a>}
    </nav>
  );
}

/** @param {FeedProps} props */
export function Feed(props) {
  const [state, setState] = useState(/** @type {FeedState} */ ({...props}));
  const [homeDirty, setHomeDirty] = useState(false);

  const context = useContext(FeedContext);

  const refreshNewestHome = useCallback(() => {
    return getJSON('/').then(data => {
      setState(current => ({...current, ...data}));
      setHomeDirty(false);
      return data;
    });
  }, []);

  useEffect(() => {
    if (props.realtime_enabled !== true || props.realtime_home !== true) return undefined;

    /** @type {EventSource | null} */
    let source = null;
    const markTimelineDirty = () => {
      setHomeDirty(true);
    };
    const markNotificationsDirty = () => {
      const badge = document.getElementById('notification-badge');
      if (badge) {
        badge.hidden = false;
        badge.title = 'New notifications';
      }
    };
    const closeRealtime = () => {
      if (source) {
        source.close();
        source = null;
      }
    };
    const openRealtime = () => {
      if (source || document.visibilityState === 'hidden' || typeof EventSource === 'undefined') return;
      source = new EventSource('/a/events');
      source.addEventListener('timeline-dirty', markTimelineDirty);
      source.addEventListener('notifications-dirty', markNotificationsDirty);
    };
    const handleVisibility = () => {
      if (document.visibilityState === 'hidden') {
        closeRealtime();
      } else {
        openRealtime();
      }
      if (document.visibilityState !== 'hidden') {
        refreshNewestHome().catch(error => console.error(error));
      }
    };

    openRealtime();
    document.addEventListener('visibilitychange', handleVisibility);
    const reconcile = setInterval(() => {
      if (document.visibilityState !== 'hidden') {
        refreshNewestHome().catch(error => console.error(error));
      }
    }, 180 * 1000);

    return () => {
      clearInterval(reconcile);
      document.removeEventListener('visibilitychange', handleVisibility);
      closeRealtime();
    };
  }, [props.realtime_enabled, props.realtime_home, refreshNewestHome]);

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
                  private={feed.private}
                  commands={feed.commands}
                  manageServicesUrl={state.manage_services_url}
                  groupSettingsUrl={state.group_settings_url}
                  groupMembersUrl={state.group_members_url} />
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
        <OnPageEditor feedUuid={feed.uuid} responseMode="list" postEntry={onPostEntry} />
      </Suspense>
    )
  }

  /** @type {React.ReactNode} */
  var feedPaginNodes = null;
  if (state.show_paging === true) {
    feedPaginNodes = (
      <FeedPagin show={state.show_paging} prev={state.prev_start}
                 next={state.next_start} query={state.query}
                 showPrev={state.show_prev} showNext={state.show_next}
                 cursorPaging={state.cursor_paging}
                 nextCursor={state.next_cursor} />
    )
  }

  return (
    <FeedContext.Provider value={config}>
      {feedHeader}
      <div id="feed" className="feed">
		{editorNodes}
        {homeDirty && props.realtime_home === true && (
          <div className="notification home-dirty-banner" role="status">
            <button type="button" className="inline-action"
                    onClick={() => refreshNewestHome().catch(error => console.error(error))}>
              有新动态，点击刷新
            </button>
          </div>
        )}
        {entryNodes}
        {feedPaginNodes}
      </div>
    </FeedContext.Provider>
  );
}

/** @param {{data: AppData}} props */
export function App({data}) {
  var url = window.location.pathname + window.location.search;
  const appData = data;
  var feedData = appData.feed;
  return (
    <Feed url={url} feed={feedData}
      show_header={appData.show_header}
      show_paging={appData.show_paging}
      show_share={appData.show_share}
      prev_start={appData.prev_start}
      next_start={appData.next_start}
      show_prev={appData.show_prev}
      show_next={appData.show_next}
      cursor_paging={appData.cursor_paging}
      next_cursor={appData.next_cursor}
      realtime_enabled={appData.realtime_enabled}
      realtime_home={appData.realtime_home}
      manage_services_url={appData.manage_services_url}
      group_settings_url={appData.group_settings_url}
      group_members_url={appData.group_members_url}
      query={appData.query}
      onpage={appData.onpage}
      onpage_edit={appData.onpage_edit} />
  );
}
