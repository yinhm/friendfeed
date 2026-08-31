import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AccountApp, tabFromPath } from './account';

const profile = {
  uuid: 'c6f8dca854f011ddb489003048343a40',
  id: 'oldname',
  name: 'Old Name',
  type: 'user',
};

const services = {
  twitter: { id: 'twitter', name: 'Twitter', username: 'ffuser' },
};

const jsonResponse = (value) => ({ ok: true, json: vi.fn().mockResolvedValue(value) });

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('AccountApp', () => {
  it('shows the profile tab first when tab=profile', () => {
    render(<AccountApp initialTab="profile" profile={profile} services={services} />);
    expect(screen.getByLabelText(/Profile ID/)).toHaveValue('oldname');
    expect(screen.queryByText('Twitter')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Edit Profile' }))
      .toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('link', {name: 'API'}))
      .toHaveAttribute('href', '/feed/oldname/api');
  });

  it('shows the import tab first when tab=import', () => {
    render(<AccountApp initialTab="import" profile={profile} services={services} />);
    expect(screen.getByText('Twitter')).toBeInTheDocument();
    expect(screen.queryByLabelText(/Profile ID/)).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Import Services' }))
      .toHaveAttribute('aria-current', 'page');
  });

  it('switches tabs client-side and syncs the URL', () => {
    const pushState = vi.spyOn(window.history, 'pushState').mockImplementation(() => {});
    render(<AccountApp initialTab="profile" profile={profile} services={services} />);

    fireEvent.click(screen.getByRole('link', { name: 'Import Services' }));
    expect(screen.getByText('Twitter')).toBeInTheDocument();
    expect(pushState).toHaveBeenCalledWith({ tab: 'import' }, '', '/account/import');

    fireEvent.click(screen.getByRole('link', { name: 'Edit Profile' }));
    expect(screen.getByLabelText(/Profile ID/)).toBeInTheDocument();
    expect(pushState).toHaveBeenCalledWith({ tab: 'profile' }, '', '/account/profile');
  });

  it('keeps saved profile values across tab switches', async () => {
    const saved = { ...profile, id: 'newname', name: 'New Name' };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(saved));
    vi.stubGlobal('fetch', fetchMock);
    const { container } = render(
      <AccountApp initialTab="profile" profile={profile} services={services} />);

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'newname' } });
    fireEvent.change(screen.getByLabelText(/Display Name/), { target: { value: 'New Name' } });
    fireEvent.submit(container.querySelector('form'));
    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument());

    fireEvent.click(screen.getByRole('link', { name: 'Import Services' }));
    fireEvent.click(screen.getByRole('link', { name: 'Edit Profile' }));

    // Regression: a remount must not resurrect the server-injected snapshot.
    expect(screen.getByLabelText(/Profile ID/)).toHaveValue('newname');
    expect(screen.getByLabelText(/Display Name/)).toHaveValue('New Name');
    expect(screen.getByRole('link', {name: 'API'}))
      .toHaveAttribute('href', '/feed/newname/api');
  });

  it('keeps service removals across tab switches', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ deleted: 'twitter' }));
    vi.stubGlobal('fetch', fetchMock);
    render(<AccountApp initialTab="import" profile={profile} services={services} />);

    fireEvent.click(screen.getByRole('button', { name: 'Remove' }));
    fireEvent.click(screen.getAllByRole('button', {name: 'Remove', hidden: true})[1]);
    await waitFor(() => expect(screen.queryByText('Twitter')).not.toBeInTheDocument());

    fireEvent.click(screen.getByRole('link', { name: 'Edit Profile' }));
    fireEvent.click(screen.getByRole('link', { name: 'Import Services' }));

    expect(screen.queryByText('Twitter')).not.toBeInTheDocument();
    expect(screen.getByText('No services connected yet.')).toBeInTheDocument();
  });

  it('clears the onboarding banner and welcome param after the first save', async () => {
    const generated = { ...profile, id: 'ff-x7k2p9qm' };
    const saved = { ...generated, id: 'alice' };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(saved));
    vi.stubGlobal('fetch', fetchMock);
    const replaceState = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});
    const { container } = render(
      <AccountApp initialTab="profile" profile={generated} services={services} welcome={true} />);

    expect(screen.getByRole('note')).toHaveTextContent('generated automatically');

    fireEvent.change(screen.getByLabelText(/Profile ID/), { target: { value: 'alice' } });
    fireEvent.submit(container.querySelector('form'));
    await waitFor(() => expect(screen.getByRole('status')).toBeInTheDocument());

    // The banner must not keep claiming the renamed ID was generated.
    expect(screen.queryByRole('note')).not.toBeInTheDocument();
    expect(replaceState).toHaveBeenCalledWith({ tab: 'profile' }, '', '/account/profile');
  });

  it('stamps the initial history entry for Back-button restores', () => {
    const replaceState = vi.spyOn(window.history, 'replaceState').mockImplementation(() => {});
    render(<AccountApp initialTab="profile" profile={profile} services={services} />);
    expect(replaceState).toHaveBeenCalledWith({ tab: 'profile' }, '');
  });

  it('restores the tab from the URL when history state is missing', () => {
    render(<AccountApp initialTab="profile" profile={profile} services={services} />);

    // Browser Back to an entry without state (e.g. the original page load
    // before replaceState): the URL decides the tab.
    window.history.pushState(null, '', '/account/import');
    fireEvent.popState(window, { state: null });
    expect(screen.getByText('Twitter')).toBeInTheDocument();

    window.history.pushState(null, '', '/account/profile');
    fireEvent.popState(window, { state: null });
    expect(screen.getByLabelText(/Profile ID/)).toBeInTheDocument();
  });
});

describe('tabFromPath', () => {
  it('maps account paths to tabs', () => {
    expect(tabFromPath('/account/import')).toBe('import');
    expect(tabFromPath('/account/import/')).toBe('import');
    expect(tabFromPath('/account/profile')).toBe('profile');
    expect(tabFromPath('/account')).toBe('profile');
  });
});
