// Keep the Pongo2 sidebar usable on anonymous Feed pages. Authenticated pages
// render their navigation through SiteLayout and must not use this fallback.
export function initSSRNavigation() {
  const navigation = document.querySelector('body > .page .sidebar details.menu');
  if (!navigation || !window.matchMedia) return;

  const media = window.matchMedia('(max-width: 600px)');
  const sync = () => {
    navigation.open = !media.matches;
  };
  sync();
  media.addEventListener('change', sync);
}
