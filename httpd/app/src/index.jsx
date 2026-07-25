// @ts-check

import React from 'react';
import { createRoot } from "react-dom/client";

import './index.css';
import './App.css';
import './styles/globals.css';
import { App } from './App';
import { Search } from './search';
import { ProfileApp } from './profile';
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

const profileEl = document.getElementById("profile-root");
if (profileEl) {
  createRoot(profileEl).render(
    <React.StrictMode>
      <ProfileApp />
    </React.StrictMode>
  );
}

initNavigation();
