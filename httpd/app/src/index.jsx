import React from 'react';
import { createRoot } from "react-dom/client";

import './index.css';
import './App.css';
import './styles/globals.css';
import { App } from './App';
import { Search } from './search';
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

initNavigation();
