# AGENTS.md

## Conventions

- Keep JavaScript out of the HTML templates (`templates/*.html`): no inline
  `<script>` blocks and no inline event handlers. Page behavior belongs in the
  React app (`app/src/`), which pages load as an external bundle via
  `{% block scripts %}`. Server-rendered data bootstrapping (e.g. the
  `window.appData` assignment in `feed.html`) is the existing exception.
