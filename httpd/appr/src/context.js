import React from 'react';

export const FeedContext = React.createContext({
  config: {
    show_header: false,
    show_paging: false,
    show_share: false,
    onpage_edit: false,
  },
});
