// @ts-check
import React from 'react';
import {GroupNav} from './group-create';

/** @param {{data: import('./browser-types').GroupsPageData}} props */
export function GroupsPage({data}) {
  return <div className="feed">
    <h2 className="page-title">{data.heading}</h2>
    {data.current_user_id && <GroupNav currentUserId={data.current_user_id}
      active={data.page === 'mine' ? 'mine' : 'discover'} />}
    {data.groups.length > 0
      ? <ul className="item-list groups-list">{data.groups.map(group => <li key={group.id}>
          <img className="avatar" src={group.picture} alt="" />
          <a href={`/feed/${group.id}`} title={group.id}>{group.name}</a>
          {group.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
          {group.description && <span className="muted">{group.description}</span>}
        </li>)}</ul>
      : <p className="muted">{data.empty_text}</p>}
    {data.next_cursor && <p className="pagination"><a href={`/groups?cursor=${encodeURIComponent(data.next_cursor)}`}>Next »</a></p>}
  </div>;
}
