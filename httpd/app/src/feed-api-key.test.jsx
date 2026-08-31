import React from 'react';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import FeedApiKeyPage from './feed-api-key';

const feed = {id: 'book-club', name: 'Book Club', type: 'group'};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it('renders group management navigation and generates a one-time token', async () => {
  const fetchMock = vi.fn().mockResolvedValue({ok: true, json: async () => ({
    status: {active: true, key_id: 'safe-id', created_at_ms: 1}, token: 'ffk1_secret',
  })});
  vi.stubGlobal('fetch', fetchMock);
  render(<FeedApiKeyPage data={{feed, status: {active: false}}} />);
  expect(screen.getByRole('link', {name: 'API'})).toHaveAttribute('aria-current', 'page');
  fireEvent.click(screen.getAllByRole('button', {name: 'Generate', hidden: true}).at(-1));
  await screen.findByText('ffk1_secret');
  expect(fetchMock).toHaveBeenCalledWith('/feed/book-club/api/generate', expect.objectContaining({method: 'POST'}));
  expect(screen.getByText(/will not be shown again/i)).toBeInTheDocument();
});

it('copies the one-time token without persisting it in page data', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText}});
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ok: true, json: async () => ({
    status: {active: true, key_id: 'safe-id'}, token: 'ffk1_copy_only_once',
  })}));
  render(<FeedApiKeyPage data={{feed, status: {active: false}}} />);
  fireEvent.click(screen.getAllByRole('button', {name: 'Generate', hidden: true}).at(-1));
  await screen.findByText('ffk1_copy_only_once');
  fireEvent.click(screen.getByRole('button', {name: 'Copy'}));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('ffk1_copy_only_once'));
});

it('requires confirmation before rotating and revoking an active key', () => {
  render(<FeedApiKeyPage data={{feed, status: {active: true, key_id: 'safe'}}} />);
  const rotate = screen.getAllByRole('button', {name: 'Rotate', hidden: true});
  const revoke = screen.getAllByRole('button', {name: 'Revoke', hidden: true});
  expect(rotate).toHaveLength(2);
  expect(revoke).toHaveLength(2);
  expect(rotate[0]).toHaveAttribute('popovertarget');
  expect(revoke[0]).toHaveAttribute('popovertarget');
});

it('keeps a failed mutation visible as a page error', async () => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ok: false, json: async () => ({error: 'denied'})}));
  render(<FeedApiKeyPage data={{feed: {...feed, type: 'user'}, status: {active: true, key_id: 'safe'}}} />);
  fireEvent.click(screen.getAllByRole('button', {name: 'Revoke', hidden: true}).at(-1));
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('denied'));
  expect(screen.queryByText(/copy this token/i)).not.toBeInTheDocument();
});

it('uses account navigation for a personal Feed API key', () => {
  render(<FeedApiKeyPage data={{feed: {id: 'alice', name: 'Alice', type: 'user'},
    status: {active: false}}} />);
  expect(screen.getByRole('navigation', {name: 'Account management'})).toBeInTheDocument();
  expect(screen.getByRole('link', {name: 'Edit Profile'})).toHaveAttribute('href', '/account/profile');
  expect(screen.getByRole('link', {name: 'API'})).toHaveAttribute('aria-current', 'page');
});
