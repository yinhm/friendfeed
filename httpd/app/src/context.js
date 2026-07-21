import React from 'react';

export const FeedContext = React.createContext({
  show_header: false,
  show_paging: false,
  show_share: false,
  onpage: false,
  onpage_edit: false,
  feed_uuid: "",
  toggleEditor: () => {
    console.log("toggle editor");
  },
});