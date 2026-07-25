// @ts-check

import React, {useEffect, useState} from 'react';
import {ProfileForm} from './profile';
import {ImportPanel} from './import';

/**
 * @typedef {import('./profile').ProfileData} ProfileData
 * @typedef {import('./import').ServiceData} ServiceData
 * @typedef {'profile' | 'import'} AccountTab
 * @typedef {object} AccountData
 * @property {AccountTab} tab
 * @property {ProfileData} profile
 * @property {Record<string, ServiceData>} services
 */

/** @type {Record<AccountTab, string>} */
const TAB_PATHS = {
  profile: '/account/profile',
  import: '/account/import',
};

/**
 * Unified account page: profile editing and import services as tabs in a
 * single app. Tab switches are client-side and keep the URL in sync so a
 * refresh or direct link lands on the same tab.
 *
 * @param {{initialTab: AccountTab, profile: ProfileData,
 * services: Record<string, ServiceData>}} props
 */
export function AccountApp(props) {
  const [tab, setTab] = useState(props.initialTab);

  useEffect(() => {
    /** @param {PopStateEvent} event */
    const onPopState = (event) => {
      const stateTab = /** @type {{tab?: AccountTab} | null} */ (event.state)?.tab;
      if (stateTab) {
        setTab(stateTab);
      }
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  /**
   * @param {AccountTab} next
   * @param {React.MouseEvent<HTMLAnchorElement>} event
   */
  const switchTab = (next, event) => {
    event.preventDefault();
    setTab(next);
    window.history.pushState({tab: next}, '', TAB_PATHS[next]);
  };

  /** @param {AccountTab} name @param {string} label */
  const tabLink = (name, label) => {
    const active = tab === name;
    return (
      <a href={TAB_PATHS[name]}
         aria-current={active ? 'page' : undefined}
         onClick={(e) => switchTab(name, e)}
         className={active
           ? 'border-b-2 border-blue-600 px-3 py-2 text-sm font-medium text-blue-600'
           : 'border-b-2 border-transparent px-3 py-2 text-sm text-gray-600 hover:text-gray-900'}>
        {label}
      </a>
    );
  };

  return (
    <div>
      <nav className="mb-6 flex gap-1 border-b border-gray-200">
        {tabLink('profile', 'Edit Profile')}
        {tabLink('import', 'Import Services')}
      </nav>
      {tab === 'profile'
        ? <ProfileForm profile={props.profile} />
        : <ImportPanel services={props.services} />}
    </div>
  );
}

export function AccountPage() {
  const data = /** @type {Window & {accountData: AccountData}} */ (
    /** @type {unknown} */ (window)
  ).accountData;
  return <AccountApp initialTab={data.tab} profile={data.profile} services={data.services} />;
}
