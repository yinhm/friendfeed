// @ts-check

import React from 'react';
import {AccountPage} from './account';
import {FeedImportPage} from './import';

/** @param {{page: string, data: unknown}} props */
export default function AccountPages({page, data}) {
  if (page === 'account') {
    return <AccountPage data={/** @type {NonNullable<Parameters<typeof AccountPage>[0]>['data']} */ (data)} />;
  }
  return <FeedImportPage data={/** @type {import('./browser-types').FeedImportPageData} */ (data)} />;
}
