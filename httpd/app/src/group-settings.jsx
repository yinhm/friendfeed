// @ts-check
import React from 'react';
import {
  confirmationActions,
  confirmationPopover,
  obsidianButton,
  outlinedButton,
  primaryButton,
} from './button-styles';
import {GroupManagementNav} from './group-management';
import {AvatarUpload} from './avatar-upload';
import {postJSON} from './utils';

/** @param {{data: import('./browser-types').GroupSettingsPageData}} props */
export function GroupSettingsPage({data}) {
  const group = data.group;
  /** @param {string} action @param {string} token */
  const saveAvatar = (action, token) => postJSON('/a/group-avatar', {
    group_id: group.id, picture_action: action, picture_asset_token: token,
  });
  return <div className="feed">
    <h2 className="page-title">Group settings: {group.name}
      {group.private && <span className="private-icon" role="img" aria-label="Private" title="Private" />}
    </h2>
    <GroupManagementNav groupId={group.id} active="settings" />
    <form method="post" action={`/groups/${group.id}/settings`} className="ff-form">
      {data.error && <div className="error-banner" role="alert">{data.error}</div>}
      <div className="field">
        <label htmlFor="group-name">Group Name *</label>
        <input type="text" id="group-name" name="name" required maxLength={64} defaultValue={group.name} />
      </div>
      <div className="field">
        <label htmlFor="group-description">Description</label>
        <textarea id="group-description" name="description" rows={4} maxLength={500} defaultValue={group.description} />
      </div>
      <AvatarUpload picture={group.picture} initialAction={data.picture_action}
        initialToken={data.picture_asset_token} autoSave={saveAvatar} />
      <div className="actions">
        <button type="submit" className={primaryButton}>Save</button>
        <a href={`/feed/${group.id}`}>Cancel</a>
      </div>
    </form>
    <div className="danger-zone">
      <h3>Delete this group</h3>
      <p className="hint">The group is soft-deleted: it immediately blocks joins, posting and new deliveries. Historical content is cleaned up in the background.</p>
      <button type="button" className={obsidianButton} popoverTarget="delete-group-confirmation">Delete group</button>
      <div id="delete-group-confirmation" className={confirmationPopover} popover="auto">
        <p><strong>Delete {group.name}?</strong></p>
        <p className="hint">This action immediately disables the group and cannot be undone here.</p>
        <div className={confirmationActions}>
          <form method="post" action={`/groups/${group.id}/delete`}>
            <button type="submit" className={obsidianButton}>Delete</button>
          </form>
          <button type="button" className={outlinedButton} popoverTarget="delete-group-confirmation" popoverTargetAction="hide">Cancel</button>
        </div>
      </div>
    </div>
  </div>;
}
