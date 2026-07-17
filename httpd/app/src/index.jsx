import React from 'react';
import { createRoot } from "react-dom/client";

import './index.css';
import './App.css';
import './styles/globals.css';
import { App } from './App';
import { Search } from './search';
import { initNavigation } from './navigation';

// Guard every mount: pages without the sidebar have no #search element, and
// a throw here marks the module as errored — which then makes lazy chunks
// fail too, because they import from this entry module.
// The flag also keeps the module idempotent: in production the page loads
// bundle.min.js (a copy) while lazy chunks import ./index.js, so this module
// can be evaluated twice.
if (!window.__ffdbMounted) {
  window.__ffdbMounted = true;

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
}

