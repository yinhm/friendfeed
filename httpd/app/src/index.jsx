// @ts-check

import React, {lazy, Suspense} from 'react';
import { createRoot } from "react-dom/client";

import './styles/globals.css';
import { App } from './App';
import { SiteLayout } from './site-layout';
import { initSSRNavigation } from './ssr-navigation';

const AccountPages = lazy(() => import('./account-pages'));
const GroupPages = lazy(() => import('./group-pages'));
const NotificationPages = lazy(() => import('./notification-pages'));

/** @typedef {import('./browser-types').PageBootstrap} PageBootstrap */

function BootstrapError() {
  return <div className="feed" role="alert">Unable to load this page.</div>;
}

function PageLoading() {
  return <div className="feed" role="status">Loading…</div>;
}

/** @param {PageBootstrap} bootstrap */
function PageDispatcher({page, data, current_user: currentUser}) {
  if (page === 'feed') return <App data={/** @type {import('./browser-types').FeedPageData} */ (data)} />;
  if (page === 'account' || page === 'feed-import') {
    return <Suspense fallback={<PageLoading />}><AccountPages page={page} data={data} /></Suspense>;
  }
  if (page === 'notifications' || page === 'requests') {
    return <Suspense fallback={<PageLoading />}><NotificationPages page={page} data={data} /></Suspense>;
  }
  if (page === 'group-create' || page === 'group-settings' || page === 'group-members' || page === 'groups') {
    return <Suspense fallback={<PageLoading />}><GroupPages page={page} data={data} currentUserId={currentUser?.id ?? ''} /></Suspense>;
  }
  return <BootstrapError />;
}

const rootEl = document.getElementById("app-root");
if (rootEl) {
  const bootstrap = /** @type {Window & {pageBootstrap?: PageBootstrap}} */ (
    /** @type {unknown} */ (window)
  ).pageBootstrap;
  const valid = bootstrap?.version === 1 && typeof bootstrap.page === 'string' && bootstrap.data != null;
  if (valid && !bootstrap.layout) initSSRNavigation();
  createRoot(rootEl).render(
    <React.StrictMode>
      {valid ? (bootstrap.layout
        ? <SiteLayout bootstrap={bootstrap}><PageDispatcher {...bootstrap} /></SiteLayout>
        : <PageDispatcher {...bootstrap} />) : <BootstrapError />}
    </React.StrictMode>
  );
}
