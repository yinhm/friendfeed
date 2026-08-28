// @ts-check
import React from 'react';

const activeNav = 'border-b-2 border-primary px-3 py-2 text-sm font-medium text-primary';
const inactiveNav = 'border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground hover:text-foreground';

/** @param {{groupId: string, active: 'feed' | 'settings' | 'members' | 'import', canManage?: boolean}} props */
export function GroupManagementNav({groupId, active, canManage = true}) {
  /** @param {'feed' | 'settings' | 'members' | 'import'} name @param {string} href @param {string} label */
  const link = (name, href, label) => <a href={href} aria-current={active === name ? 'page' : undefined}
    className={active === name ? activeNav : inactiveNav}>{label}</a>;
  return <nav className="mb-6 flex gap-1 border-b border-border" aria-label="Feed management">
    {link('feed', `/feed/${groupId}`, 'Feed')}
    {canManage && link('settings', `/groups/${groupId}/settings`, 'Settings')}
    {link('members', `/groups/${groupId}/members`, 'Members')}
    {canManage && link('import', `/feed/${groupId}/import`, 'Import Services')}
  </nav>;
}
