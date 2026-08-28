import React from 'react';
import {render, screen} from '@testing-library/react';
import {SiteLayout} from './site-layout';

it('renders authenticated navigation, notification state, groups, archive, and search in one tree', () => {
  render(<SiteLayout bootstrap={{version: 1, page: 'groups', current_user: {uuid: 'u', id: 'alice', name: 'Alice'},
    layout: {onpage: false, has_unread_notifications: true, show_groups: true,
      groups: [{id: 'secret', name: 'Secret', private: true}], archive_feed_id: 'alice',
      archive_years: [{year: 2025, count: 20, cursor: 'next/year'}]}, data: {},
  }}><div>page content</div></SiteLayout>);
  expect(screen.getByRole('link', {name: 'My feed'})).toHaveAttribute('href', '/feed/alice');
  expect(screen.getByLabelText('New notifications')).not.toHaveAttribute('hidden');
  expect(screen.getByRole('link', {name: 'Secret'})).toHaveAttribute('href', '/feed/secret');
  expect(screen.getByRole('img', {name: 'Private'})).toBeInTheDocument();
  expect(screen.getByRole('link', {name: '2025'})).toHaveAttribute('href', '/feed/alice?cursor=next%2Fyear');
  expect(screen.getByRole('link', {name: '2025'}).closest('.feed-archive-menu')).toHaveClass('sidebar-secondary-menu');
  expect(screen.getByRole('searchbox', {name: 'Search'})).toBeInTheDocument();
  expect(screen.getByText('page content')).toBeInTheDocument();
});

it('uses the compact permalink navigation when onpage is true', () => {
  render(<SiteLayout bootstrap={{version: 1, page: 'feed', layout: {
    onpage: true, has_unread_notifications: false, show_groups: false,
  }, data: {}}}><div>entry</div></SiteLayout>);
  expect(screen.getByRole('link', {name: 'Home'})).toBeInTheDocument();
  expect(screen.queryByRole('searchbox')).not.toBeInTheDocument();
});
