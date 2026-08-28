// @ts-check

import React from 'react';
import { createRoot } from "react-dom/client";

import './styles/globals.css';
import { App } from './App';
import { Search } from './search';
import { AccountPage } from './account';
import { FeedImportPage } from './import';
import { NotificationsPage } from './notifications';
import { RequestsPage } from './requests';
import { GroupCreatePage } from './group-create';
import { GroupSettingsPage } from './group-settings';
import { initNavigation } from './navigation';

/** @typedef {import('./browser-types').PageBootstrap} PageBootstrap */

function BootstrapError() {
  return <div className="feed" role="alert">Unable to load this page.</div>;
}

/** @param {PageBootstrap} bootstrap */
function PageDispatcher({page, data}) {
  if (page === 'feed') return <App data={/** @type {import('./browser-types').FeedPageData} */ (data)} />;
  if (page === 'account') return <AccountPage data={/** @type {NonNullable<Parameters<typeof AccountPage>[0]>['data']} */ (data)} />;
  if (page === 'feed-import') return <FeedImportPage data={/** @type {import('./browser-types').FeedImportPageData} */ (data)} />;
  if (page === 'notifications') return <NotificationsPage data={/** @type {import('./browser-types').NotificationsPageData} */ (data)} />;
  if (page === 'requests') return <RequestsPage data={/** @type {import('./browser-types').RequestsPageData} */ (data)} />;
  if (page === 'group-create') return <GroupCreatePage data={/** @type {import('./browser-types').GroupCreatePageData} */ (data)} />;
  if (page === 'group-settings') return <GroupSettingsPage data={/** @type {import('./browser-types').GroupSettingsPageData} */ (data)} />;
  return <BootstrapError />;
}

const rootEl = document.getElementById("app-root");
if (rootEl) {
  const bootstrap = /** @type {Window & {pageBootstrap?: PageBootstrap}} */ (
    /** @type {unknown} */ (window)
  ).pageBootstrap;
  const valid = bootstrap?.version === 1 && typeof bootstrap.page === 'string' && bootstrap.data != null;
  createRoot(rootEl).render(
    <React.StrictMode>
      {valid ? <PageDispatcher {...bootstrap} /> : <BootstrapError />}
    </React.StrictMode>
  );
}

const searchEl = document.getElementById("search");
if (searchEl) {
  createRoot(searchEl).render(
    <React.StrictMode>
      <Search />
    </React.StrictMode>
  );
}

initNavigation();
