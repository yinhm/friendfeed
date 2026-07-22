import { beforeEach, describe, expect, it, vi } from 'vitest';

import { initNavigation } from './navigation';

beforeEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('initNavigation', () => {
  it('does nothing on pages without sidebar navigation', () => {
    const matchMedia = vi.fn();
    vi.stubGlobal('matchMedia', matchMedia);
    initNavigation();
    expect(matchMedia).not.toHaveBeenCalled();
  });

  it('opens on desktop, collapses on mobile, and follows media changes', () => {
    const nav = document.createElement('details');
    nav.className = 'menu';
    nav.open = true;
    document.body.append(nav);

    const media = {
      matches: false,
      addEventListener: vi.fn(),
    };
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue(media));

    initNavigation();

    expect(window.matchMedia).toHaveBeenCalledWith('(max-width: 600px)');
    expect(nav).toHaveAttribute('open');
    expect(media.addEventListener).toHaveBeenCalledWith(
      'change',
      expect.any(Function)
    );

    media.matches = true;
    const sync = media.addEventListener.mock.calls[0][1];
    sync();
    expect(nav).not.toHaveAttribute('open');

    media.matches = false;
    sync();
    expect(nav).toHaveAttribute('open');
  });
});
