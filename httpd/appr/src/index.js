import React from 'react';
import ReactDOM from 'react-dom';
import ReactDOMServer from 'react-dom/server';
import './index.css';
// import App from './App';
import {Feed} from './App';
import reportWebVitals from './reportWebVitals';

var path = window.location.pathname + window.location.search;
var feedData = window.app_props.feed;
ReactDOM.render(
  <React.StrictMode>
    {/* <App /> */}
    <Feed url={path} feed={feedData} />
  </React.StrictMode>,
  document.getElementById('feed')
);


export function RenderFeedComponent(props) {
  const body = ReactDOMServer.renderToString(
    React.createElement(Feed, props)
  );
  return body;
};


// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();
