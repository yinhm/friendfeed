import {initSSRNavigation} from './ssr-navigation';

it('collapses anonymous SSR navigation on mobile and reopens it on desktop', () => {
  document.body.innerHTML = '<div class="page"><div class="sidebar"><details class="menu" open></details></div></div>';
  const listeners = [];
  const media = {matches: true, addEventListener: (_name, listener) => listeners.push(listener)};
  window.matchMedia = vi.fn(() => media);

  initSSRNavigation();
  const navigation = document.querySelector('details.menu');
  expect(navigation.open).toBe(false);

  media.matches = false;
  listeners[0]();
  expect(navigation.open).toBe(true);
});

it('does not touch navigation outside the SSR page shell', () => {
  document.body.innerHTML = '<details class="menu" open></details>';
  window.matchMedia = vi.fn();
  initSSRNavigation();
  expect(window.matchMedia).not.toHaveBeenCalled();
});
