// @ts-check
import React from 'react';

/** @param {{data: import('./browser-types').NotificationsPageData}} props */
export function NotificationsPage({data}) {
  return <div className="feed">
    <h2 className="page-title">Notifications</h2>
    {data.items.length > 0
      ? <ul className="item-list notifications-list">
          {data.items.map((item, index) => <li key={`${item.href}-${item.date}-${index}`}>
            <span className="item-time">{item.date}</span>
            <a href={item.href}>{item.text}</a>
          </li>)}
        </ul>
      : <p className="muted">No notifications yet.</p>}
    {data.next_cursor && <p><a href={`/notifications?cursor=${encodeURIComponent(data.next_cursor)}`}>Older notifications</a></p>}
  </div>;
}
