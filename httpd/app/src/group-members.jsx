// @ts-check
import React from 'react';
import {GroupManagementNav} from './group-management';

/** @param {{profile: import('./browser-types').AccountPageData['profile']}} props */
function ProfileLink({profile}) {
  return <div className="flex min-w-0 flex-1 items-center gap-2">
    <img src={profile.picture} className="avatar flex-none" alt="" />
    <a href={`/feed/${profile.id}`} className="truncate">{profile.name}</a>
    {profile.private && <span className="private-icon flex-none" role="img" aria-label="Private" title="Private" />}
  </div>;
}

const primaryButton = 'min-w-20 rounded-full bg-[#1c1917] px-4 py-2 text-sm font-semibold text-white hover:bg-[#292524] disabled:opacity-50';
const activeButton = 'min-w-20 rounded-full border border-stone-300 bg-white px-4 py-2 text-sm font-semibold text-[#1c1917] hover:bg-stone-50 disabled:opacity-50';

/** @param {{action: string, member: import('./browser-types').GroupMembersPageData['members'][number]}} props */
function MemberActions({action, member}) {
  const profile = member.profile;
  const adminConfirmation = `revoke-admin-${profile.uuid}`;
  const removeConfirmation = `remove-member-${profile.uuid}`;
  return <div className="ml-auto grid grid-cols-2 gap-2">
    {member.is_admin
      ? <>
          <button type="button" className={activeButton} popoverTarget={adminConfirmation}>Admin</button>
          <span className="invisible min-w-20" aria-hidden="true">Remove</span>
          <div id={adminConfirmation} popover="auto" className="destructive-confirmation border-stone-300">
            <p><strong>Revoke admin access from {profile.name}?</strong></p>
            <form method="post" action={action} className="mt-4 flex gap-2">
              <input type="hidden" name="target_uuid" value={profile.uuid} />
              <button type="submit" name="action" value="demote" className={primaryButton}>Revoke</button>
              <button type="button" className={activeButton} popoverTarget={adminConfirmation}
                      popoverTargetAction="hide">Cancel</button>
            </form>
          </div>
        </>
      : <>
          <form method="post" action={action}>
            <input type="hidden" name="target_uuid" value={profile.uuid} />
            <button type="submit" name="action" value="promote" className={primaryButton}>Admin</button>
          </form>
          <button type="button" className={primaryButton} popoverTarget={removeConfirmation}>Remove</button>
          <div id={removeConfirmation} popover="auto" className="destructive-confirmation border-stone-300">
            <p><strong>Remove {profile.name} from this group?</strong></p>
            <form method="post" action={action} className="mt-4 flex gap-2">
              <input type="hidden" name="target_uuid" value={profile.uuid} />
              <button type="submit" name="action" value="remove" className={primaryButton}>Remove</button>
              <button type="button" className={activeButton} popoverTarget={removeConfirmation}
                      popoverTargetAction="hide">Cancel</button>
            </form>
          </div>
        </>}
  </div>;
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
          {data.can_manage && <MemberActions action={action} member={member} />}
        </li>)}</ul>
    {data.has_more && <p className="muted">This group has more members than shown here.</p>}
  </div>;
}
