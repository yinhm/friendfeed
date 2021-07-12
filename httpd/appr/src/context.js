import React from 'react';

export const FeedContext = React.createContext({
  show_header: false,
  show_paging: false,
  show_share: false,
  onpage_edit: false,
  feed_id: "",
  toggleEditor: () => {
    console.log("toggle editor");
  },
});