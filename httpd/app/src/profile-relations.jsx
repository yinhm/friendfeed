// @ts-check
import React from 'react';
import {ProfileRelationsNav} from './App';

/** @param {{data: import('./browser-types').ProfileRelationsPageData}} props */
export function ProfileRelationsPage({data}) {
  const heading = data.relation === 'following' ? 'Following' : 'Followers';
  return <div className="feed">
    <ProfileRelationsNav feedId={data.profile.id} active={data.relation} />
    <h2 className="page-title">{heading}</h2>
    {data.profiles.length > 0
      ? <ul className="item-list groups-list">{data.profiles.map(profile => <li key={profile.id}>
          <img className="avatar" src={profile.picture} alt="" />
          <a href={`/feed/${profile.id}`} title={profile.id}>{profile.name}</a>
          {profile.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
        </li>)}</ul>
      : <p className="muted">No {heading.toLowerCase()} yet.</p>}
  </div>;
}
