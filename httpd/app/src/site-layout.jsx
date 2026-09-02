// @ts-check
import React, {useEffect, useRef} from 'react';
import {Search} from './search';

/** @param {{bootstrap: import('./browser-types').PageBootstrap, children: React.ReactNode}} props */
export function SiteLayout({bootstrap, children}) {
  const currentUser = bootstrap.current_user;
  const layout = bootstrap.layout ?? {onpage: false, has_unread_notifications: false, show_groups: false};
  const menuRef = useRef(/** @type {HTMLDetailsElement | null} */ (null));

  useEffect(() => {
    if (!menuRef.current || !window.matchMedia) return undefined;
    const media = window.matchMedia('(max-width: 600px)');
    const sync = () => {
      if (media.matches) menuRef.current?.removeAttribute('open');
      else menuRef.current?.setAttribute('open', '');
    };
    sync();
    media.addEventListener('change', sync);
    return () => media.removeEventListener('change', sync);
  }, []);

  if (layout.onpage) {
    return <><div className="topmenu"><a href="/" className="nav-heading">Home</a>
      <ul className="nav-list"><li><a href="/public">Public</a></li></ul></div>
      <div className="page"><div className="main">{children}</div></div></>;
  }

  return <div className="page">
    <div className="main">{children}</div>
    <div className="sidebar">
      <details className="menu" open ref={menuRef}>
        <summary>FriendFeed</summary>
        <div className="section"><ul>
          <li><a href="/">Home</a></li>
          {currentUser && <>
            <li><a href={`/feed/${currentUser.id}`}>My feed</a></li>
            <li><a href="/notifications">Notifications<span id="notification-badge" className="notification-badge"
              role="img" aria-label="New notifications" hidden={!layout.has_unread_notifications} /></a></li>
            <li><a href={`/feed/${currentUser.id}/likes`}>Likes</a></li>
            <li><a href={`/feed/${currentUser.id}/comments`}>Comments</a></li>
            <li><a href="/public">Public</a></li>
          </>}
          <li><a href="/groups">Groups</a></li>
        </ul></div>
        <div className="section"><h3>Account</h3><ul>
          {currentUser ? <>
            <li><a href="/account/profile">Profile</a></li>
            <li><a href="/account/requests">Requests</a></li>
            <li><a href="/logout">Logout</a></li>
          </> : <>
            <li><a href="/auth/twitter">Twitter</a></li>
            <li><a href="/auth/google">Google</a></li>
          </>}
        </ul></div>
        {currentUser && <Search />}
      </details>
      {currentUser && layout.show_groups && <div className="menu groups-menu sidebar-secondary-menu"><div className="section">
        <h3 className="groups-heading">Groups</h3>
        {layout.groups && layout.groups.length > 0 && <ul>{layout.groups.map(group => <li key={group.id}>
          <a href={`/feed/${group.id}`} title={group.id}>{group.name}</a>
          {group.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
        </li>)}</ul>}
        <div><a href={`/feed/${currentUser.id}/groups`}>More…</a></div>
      </div></div>}
      {currentUser && layout.archive_years && layout.archive_years.length > 0 &&
        <div className="menu feed-archive-menu sidebar-secondary-menu"><div className="section"><h3>Archive</h3><ul>
          {layout.archive_years.map(year => <li key={year.year}><a href={`/feed/${layout.archive_feed_id}${year.cursor ? `?cursor=${encodeURIComponent(year.cursor)}` : ''}`}>{year.year}</a>{' '}
            <span className="feed-archive-count">{year.count}</span></li>)}
        </ul></div></div>}
    </div>
  </div>;
}
