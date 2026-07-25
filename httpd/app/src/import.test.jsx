import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ImportPanel } from './import';

const services = {
  twitter: {
    id: 'twitter',
    name: 'Twitter',
    username: 'ffuser',
    profile: 'https://twitter.com/ffuser',
  },
  rss: { id: 'rss', name: 'RSS' },
};

const jsonResponse = (value) => ({ json: vi.fn().mockResolvedValue(value) });

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('ImportPanel', () => {
  it('renders connected services with usernames', () => {
    render(<ImportPanel services={services} />);
    expect(screen.getByText('Twitter')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'ffuser' })).toHaveAttribute(
      'href', 'https://twitter.com/ffuser');
    expect(screen.getByText('RSS')).toBeInTheDocument();
    // twitter already connected: no import entry point
    expect(screen.queryByRole('link', { name: 'Import Tweet' })).not.toBeInTheDocument();
  });

  it('offers the twitter import when not connected, and an empty state', () => {
    render(<ImportPanel services={{}} />);
    expect(screen.getByText('No services connected yet.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Import Tweet' }))
      .toHaveAttribute('href', '/account/import/twitter');
  });

  it('removes a service after confirmation', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ deleted: 'rss' }));
    vi.stubGlobal('fetch', fetchMock);
    render(<ImportPanel services={services} />);

    fireEvent.click(screen.getByRole('button', { name: 'remove rss' }));

    await waitFor(() => expect(screen.queryByText('RSS')).not.toBeInTheDocument());
    expect(screen.getByText('Twitter')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/account/service/rss/delete', expect.objectContaining({ method: 'GET' }));
  });

  it('keeps the service when confirmation is cancelled', () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(false));
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    render(<ImportPanel services={services} />);

    fireEvent.click(screen.getByRole('button', { name: 'remove rss' }));

    expect(screen.getByText('RSS')).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('shows the server error when removal fails', async () => {
    vi.stubGlobal('confirm', vi.fn().mockReturnValue(true));
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ error: 'service busy' }));
    vi.stubGlobal('fetch', fetchMock);
    render(<ImportPanel services={services} />);

    fireEvent.click(screen.getByRole('button', { name: 'remove rss' }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('service busy'));
    expect(screen.getByText('RSS')).toBeInTheDocument();
  });
});
