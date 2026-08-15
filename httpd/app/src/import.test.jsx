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

/** Stateful host mirroring AccountApp: owns the services map. */
function Harness({ initial }) {
  const [services, setServices] = React.useState(initial);
  return <ImportPanel services={services} target="target-uuid" onServicesChange={setServices} />;
}

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
    render(<Harness initial={services} />);

    fireEvent.click(screen.getByRole('button', { name: 'remove rss' }));

    await waitFor(() => expect(screen.queryByText('RSS')).not.toBeInTheDocument());
    expect(screen.getByText('Twitter')).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith(
      '/account/service/rss/delete?target=target-uuid', expect.objectContaining({ method: 'GET' }));
  });

  it('adds a web feed for the selected target', async () => {
    const added = {id: 'feed-id', name: 'Example', kind: 'web_feed'};
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(added));
    vi.stubGlobal('fetch', fetchMock);
    render(<Harness initial={{}} />);

    fireEvent.change(screen.getByPlaceholderText('https://example.com/feed.xml'), {
      target: {value: 'https://example.com/rss'},
    });
    fireEvent.click(screen.getByRole('button', {name: 'Add'}));

    await waitFor(() => expect(screen.getByText('Example')).toBeInTheDocument());
    expect(fetchMock).toHaveBeenCalledWith('/account/feed-service', expect.objectContaining({
      body: expect.any(URLSearchParams), method: 'POST',
    }));
    expect(fetchMock.mock.calls[0][1].body.get('target_uuid')).toBe('target-uuid');
  });

  it('shows the shared source fetch state', () => {
    render(<ImportPanel services={{rss: {
      id: 'rss', name: 'RSS', kind: 'web_feed', service_uuid: 'source',
    }}} states={{source: {last_fetch_ms: 1000, last_error: 'timeout'}}} />);
    expect(screen.getByText('Last fetch failed: timeout')).toBeInTheDocument();
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
