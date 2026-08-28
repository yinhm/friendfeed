// @ts-check
import React from 'react';
import {GroupManagementNav} from './group-management';

/** @param {{profile: import('./browser-types').AccountPageData['profile']}} props */
function ProfileLink({profile}) {
  return <><img src={profile.picture} className="avatar" alt="" />
    <a href={`/feed/${profile.id}`}>{profile.name}</a>
    {profile.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}</>;
}

/** @param {{data: import('./browser-types').GroupMembersPageData}} props */
export function GroupMembersPage({data}) {
  const action = `/groups/${data.group.id}/members/action`;
  return <div className="feed">
    <h2 className="page-title">Members of {data.group.name}
      {data.group.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
    </h2>
    <GroupManagementNav groupId={data.group.id} active="members" canManage={data.can_manage} />
    {data.error && <div className="error-banner" role="alert">{data.error}</div>}
    {data.can_manage && data.requests.length > 0 && <>
      <h3>Pending requests</h3>
      <ul className="item-list">{data.requests.map(request => <li key={request.requester.uuid}>
        <ProfileLink profile={request.requester} />
        <span className="item-time">{request.requested_at}</span>
        <form method="post" action={action}>
          <input type="hidden" name="target_uuid" value={request.requester.uuid} />
          <button type="submit" name="action" value="approve" className="legacy-button">Approve</button>
          <button type="submit" name="action" value="reject" className="legacy-button danger">Reject</button>
        </form>
      </li>)}</ul>
    </>}
    <ul className="item-list">{data.members.length === 0
      ? <li className="muted">No members.</li>
      : data.members.map(member => <li key={member.profile.uuid}>
          <ProfileLink profile={member.profile} />
          {member.is_admin && <span className="admin-badge">admin</span>}
          {data.can_manage && <form method="post" action={action}>
            <input type="hidden" name="target_uuid" value={member.profile.uuid} />
            {member.is_admin
              ? <button type="submit" name="action" value="demote" className="legacy-button">Revoke admin</button>
              : <><button type="submit" name="action" value="promote" className="legacy-button">Make admin</button>
                <button type="submit" name="action" value="remove" className="legacy-button danger">Remove</button></>}
          </form>}
        </li>)}</ul>
    {data.has_more && <p className="muted">This group has more members than shown here.</p>}
  </div>;
}
