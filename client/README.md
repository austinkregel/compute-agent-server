# Dashboard

The operator dashboard for Backup Server: a Vue 3 + Vite SPA that shows fleet
telemetry, opens remote shells, browses agent filesystems, and drives kiosk
displays. It is its own npm package, nested under `server/` so the control plane
builds and ships as one unit.

## Commands

```bash
npm install
npm run dev       # vite dev server, proxied to the Go server
npm run build     # -> dist/
npm run preview   # serve the built output
npm test          # vitest run
```

The dev server proxies `/api` to `https://localhost:8443` and `/ws` to
`wss://localhost:8443` (`proxy.config.js`). Both targets are TLS — the Go server
runs with certificates when `certs/` is present, and pointing the proxy at plain
`http`/`ws` produces connection resets that surface as a connect/disconnect loop.

## How it talks to the server

Everything live goes through one WebSocket. `src/lib/sharedWS.js` owns the
connection to `/ws/dashboard`, opens it on import, and reconnects with a doubling
backoff from 1 s to a 30 s cap. Components never open sockets of their own; they
read its reactive state and subscribe with `on(event, handler)`.

That state includes `connected`, `clientIds`, `statsMap`, `statsHistory` (15
samples per client, mirrored to `localStorage`), `capabilitiesMap`, `alertsMap`,
plus per-feature maps for Docker, kiosk, variant, log-tail, and SMS. Wire frames
are `{event, data}`; the module flattens them to `{type, ...data}` for handlers
and reverses that on send.

Authentication is ambient. The browser sends no token — the server accepts the
OIDC session cookie on the upgrade request, and `src/lib/auth.js` reflects
`/api/auth/status` into `isAuthenticated`, `isAdmin`, and `user`. Router guards
read those: `meta.requiresAuth` redirects to `/login`, and `meta.requiresAdmin`
redirects to `/`. Both gates are convenience only; the server enforces the same
rules independently on REST routes and per dashboard event.

## Layout

- `src/views/` — route-level pages
- `src/components/` — the widgets those pages compose
- `src/lib/` — `sharedWS.js` (connection and state), `auth.js` (session), `clientNav.js` (pure path/keyboard helpers)
- `src/assets/tailwind.css` — the style entry point

Tailwind 4 is wired as a Vite plugin (`@tailwindcss/vite`) with the CSS-first
`@import "tailwindcss"`. There is no PostCSS config. Dark mode is driven by
`App.vue` toggling a `dark` class on the document element, persisted in
`localStorage`. See `tailwindcss-ui-design-guidelines.md` at the repo root before
writing new component markup.

Note that `tailwind.config.js` is a leftover v3-style CommonJS file. Nothing
references it, and its `module.exports` form is invalid under this package's
`"type": "module"`, so the v4 plugin does not read it.

## Tests

Vitest with jsdom, configured in `vitest.config.mjs`, matching
`src/**/*.test.{js,ts,jsx,tsx}`. Tests sit beside the code they cover. All of
them exercise `src/lib/` — WebSocket state handling, the SMS fetch helpers, and
the navigation helpers. There are no component-mounting tests; `@vue/test-utils`
is not a dependency.

## Build output

`npm run build` writes `dist/`. The Go server locates it at runtime rather than
embedding it, trying `dist`, `../client/dist`, then `client/dist` relative to the
process working directory — so running from `server/` finds `server/client/dist`
without configuration. If no candidate exists the server logs that the SPA is
disabled and serves the API alone.

Unmatched routes fall back to `index.html` so client-side routing works on deep
links, except for asset-shaped paths, which 404 rather than returning HTML.
