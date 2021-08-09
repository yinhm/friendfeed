import React from 'react';
import ReactDOM from 'react-dom';
// import ReactDOMServer from 'react-dom/server';
import './index.css';
import './App.css';
import { App } from './App';
import { Search } from './search';
import reportWebVitals from './reportWebVitals';

if (document.getElementById('root')) {
  ReactDOM.render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
    document.getElementById('root')
  );
}

if (document.getElementById('search')) {
  ReactDOM.render(
    <React.StrictMode>
      <Search />
    </React.StrictMode>,
    document.getElementById('search')
  );
}

// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();
