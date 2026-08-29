// @ts-check

import React from 'react';
import {NotificationsPage} from './notifications';
import {RequestsPage} from './requests';

/** @param {{page: string, data: unknown}} props */
export default function NotificationPages({page, data}) {
  if (page === 'notifications') {
    return <NotificationsPage data={/** @type {import('./browser-types').NotificationsPageData} */ (data)} />;
  }
  return <RequestsPage data={/** @type {import('./browser-types').RequestsPageData} */ (data)} />;
}
