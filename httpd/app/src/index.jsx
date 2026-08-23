// @ts-check

import React from 'react';
import { createRoot } from "react-dom/client";

import './styles/globals.css';
import { App } from './App';
import { Search } from './search';
import { AccountPage } from './account';
import { FeedImportPage } from './import';
import { initNavigation } from './navigation';

// Pages without the sidebar have no #search element, so guard every mount.
const rootEl = document.getElementById("root");
if (rootEl) {
  createRoot(rootEl).render(
    <React.StrictMode>
      <App />
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

const accountEl = document.getElementById("account-root");
if (accountEl) {
  createRoot(accountEl).render(
    <React.StrictMode>
      <AccountPage />
    </React.StrictMode>
  );
}

const feedImportEl = document.getElementById("feed-import-root");
if (feedImportEl) {
  createRoot(feedImportEl).render(
    <React.StrictMode>
      <FeedImportPage />
    </React.StrictMode>
  );
}

initNavigation();
