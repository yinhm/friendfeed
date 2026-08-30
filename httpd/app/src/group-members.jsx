// @ts-check
import React from 'react';
import {
  confirmationActions,
  confirmationPopover,
  obsidianButton,
  outlinedButton,
} from './button-styles';
import {GroupManagementNav} from './group-management';

/** @param {{profile: import('./browser-types').AccountPageData['profile']}} props */
function ProfileLink({profile}) {
  return <div className="flex min-w-0 flex-1 items-center gap-2">
    <img src={profile.picture} className="avatar flex-none" alt="" />
    <a href={`/feed/${profile.id}`} className="truncate">{profile.name}</a>
    {profile.private && <span className="private-icon flex-none" role="img" aria-label="Private" title="Private" />}
  </div>;
}

const primaryButton = `min-w-20 ${obsidianButton}`;
const activeButton = `min-w-20 ${outlinedButton}`;

/** @param {{action: string, member: import('./browser-types').GroupMembersPageData['members'][number]}} props */
function MemberActions({action, member}) {
  const profile = member.profile;
  const demoteConfirmation = `demote-admin-${profile.uuid}`;
  const promoteConfirmation = `promote-admin-${profile.uuid}`;
  const removeConfirmation = `remove-member-${profile.uuid}`;
  return <div className="ml-auto grid grid-cols-2 gap-2">
    {member.is_admin
      ? <>
          <button type="button" className={activeButton} popoverTarget={demoteConfirmation}>Demote</button>
          <span className="invisible min-w-20" aria-hidden="true">Remove</span>
          <div id={demoteConfirmation} popover="auto" className={confirmationPopover}>
            <p><strong>Demote {profile.name} from admin?</strong></p>
            <form method="post" action={action} className={confirmationActions}>
              <input type="hidden" name="target_uuid" value={profile.uuid} />
              <button type="submit" name="action" value="demote" className={primaryButton}>Demote</button>
              <button type="button" className={activeButton} popoverTarget={demoteConfirmation}
                      popoverTargetAction="hide">Cancel</button>
            </form>
          </div>
        </>
      : <>
          <button type="button" className={primaryButton} popoverTarget={promoteConfirmation}>Promote</button>
          <button type="button" className={primaryButton} popoverTarget={removeConfirmation}>Remove</button>
          <div id={promoteConfirmation} popover="auto" className={confirmationPopover}>
            <p><strong>Promote {profile.name} to admin?</strong></p>
            <form method="post" action={action} className={confirmationActions}>
              <input type="hidden" name="target_uuid" value={profile.uuid} />
              <button type="submit" name="action" value="promote" className={primaryButton}>Promote</button>
              <button type="button" className={activeButton} popoverTarget={promoteConfirmation}
                      popoverTargetAction="hide">Cancel</button>
            </form>
          </div>
          <div id={removeConfirmation} popover="auto" className={confirmationPopover}>
            <p><strong>Remove {profile.name} from this group?</strong></p>
            <form method="post" action={action} className={confirmationActions}>
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
        <form method="post" action={action} className="flex gap-2">
          <input type="hidden" name="target_uuid" value={request.requester.uuid} />
          <button type="submit" name="action" value="approve" className={primaryButton}>Approve</button>
          <button type="submit" name="action" value="reject" className={activeButton}>Reject</button>
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
