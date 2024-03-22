import React from 'react';
import { createRoot } from "react-dom/client";

// import ReactDOMServer from 'react-dom/server';
import './index.css';
import './App.css';
import { App } from './App';
import { Search } from './search';
import reportWebVitals from './reportWebVitals';

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

// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();
