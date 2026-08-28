// @ts-check
import React from 'react';

const activeNav = 'border-b-2 border-primary px-3 py-2 text-sm font-medium text-primary';
const inactiveNav = 'border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground hover:text-foreground';

/** @param {{currentUserId: string, active: 'discover' | 'mine' | 'create'}} props */
export function GroupNav({currentUserId, active}) {
  /** @param {'discover' | 'mine' | 'create'} name @param {string} href @param {string} label */
  const link = (name, href, label) => <a href={href} aria-current={active === name ? 'page' : undefined}
    className={active === name ? activeNav : inactiveNav}>{label}</a>;
  return <nav className="mb-6 flex gap-1 border-b border-border" aria-label="Group navigation">
    {link('discover', '/groups', 'Discover')}
    {link('mine', `/feed/${currentUserId}/groups`, 'My Groups')}
    {link('create', '/groups/create', 'Create')}
  </nav>;
}

/** @param {{data: import('./browser-types').GroupCreatePageData}} props */
export function GroupCreatePage({data}) {
  const group = data.group;
  return <div className="feed">
    <h2 className="page-title">Create a Group</h2>
    <GroupNav currentUserId={data.current_user_id} active="create" />
    <form method="post" action="/groups/create" className="ff-form">
      {data.error && <div className="error-banner" role="alert">{data.error}</div>}
      <div className="field">
        <label htmlFor="group-id">Group ID *</label>
        <input type="text" id="group-id" name="id" required maxLength={64} pattern="[a-z0-9_-]{4,}"
          defaultValue={group.id} aria-describedby="group-id-hint" />
        <div className="hint" id="group-id-hint">Lowercase letters, digits, &quot;_&quot; and &quot;-&quot;, at least 4 characters. This is the group&apos;s URL: /feed/&lt;id&gt;. Conflicts and reserved names are reported by the server above.</div>
      </div>
      <div className="field">
        <label htmlFor="group-name">Group Name *</label>
        <input type="text" id="group-name" name="name" required maxLength={64} defaultValue={group.name} />
      </div>
      <div className="field">
        <label htmlFor="group-description">Description</label>
        <textarea id="group-description" name="description" rows={4} maxLength={500} defaultValue={group.description} />
      </div>
      <div className="field">
        <label htmlFor="group-picture">Picture URL</label>
        {group.picture && <div className="avatar-preview"><img src={group.picture} alt="" /></div>}
        <input type="url" id="group-picture" name="picture" maxLength={2048} defaultValue={group.picture} />
        <div className="hint">Full URL to the group picture (leave empty for the default).</div>
      </div>
      <div className="field">
        <label><input type="checkbox" name="private" defaultChecked={group.private} /> Private group</label>
        <div className="hint">Only approved members can read a private group. Joining requires a follow request approved by an admin.</div>
      </div>
      <div className="actions">
        <button type="submit" className="legacy-button">Create Group</button>
        <a href="/">Cancel</a>
      </div>
    </form>
  </div>;
}
