// Collapse the sidebar <details> navigation on small screens, keep it
// expanded on desktop. Pages without a sidebar (e.g. entry pages) have no
// .menu element.
export function initNavigation() {
  const nav = document.querySelector(".menu");
  if (!nav) return;

  const mq = window.matchMedia("(max-width: 600px)");
  const syncNavigation = () => {
    if (mq.matches) {
      nav.removeAttribute("open");
    } else {
      nav.setAttribute("open", "");
    }
  };
  syncNavigation();
  mq.addEventListener("change", syncNavigation);
}
