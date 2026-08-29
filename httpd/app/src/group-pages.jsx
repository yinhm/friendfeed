// @ts-check

import React from 'react';
import {GroupCreatePage} from './group-create';
import {GroupSettingsPage} from './group-settings';
import {GroupMembersPage} from './group-members';
import {GroupsPage} from './groups';

/** @param {{page: string, data: unknown, currentUserId: string}} props */
export default function GroupPages({page, data, currentUserId}) {
  if (page === 'group-create') {
    return <GroupCreatePage data={/** @type {import('./browser-types').GroupCreatePageData} */ (data)} currentUserId={currentUserId} />;
  }
  if (page === 'group-settings') {
    return <GroupSettingsPage data={/** @type {import('./browser-types').GroupSettingsPageData} */ (data)} />;
  }
  if (page === 'group-members') {
    return <GroupMembersPage data={/** @type {import('./browser-types').GroupMembersPageData} */ (data)} />;
  }
  return <GroupsPage data={/** @type {import('./browser-types').GroupsPageData} */ (data)} currentUserId={currentUserId} />;
}
