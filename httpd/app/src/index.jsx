import React from 'react';
import { createRoot } from "react-dom/client";

import './index.css';
import './App.css';
import './styles/globals.css';
import { App } from './App';
import { Search } from './search';

const root = createRoot(document.getElementById("root"));
root.render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);

const search = createRoot(document.getElementById("search"));
search.render(
  <React.StrictMode>
    <Search />
  </React.StrictMode>
);

