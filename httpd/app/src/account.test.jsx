import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AccountApp } from './account';

const profile = {
  uuid: 'c6f8dca854f011ddb489003048343a40',
  id: 'oldname',
  name: 'Old Name',
  type: 'user',
};

const services = {
  twitter: { id: 'twitter', name: 'Twitter', username: 'ffuser' },
};

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
});
