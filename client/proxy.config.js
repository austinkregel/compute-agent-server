// Vite dev server proxy config for API requests to Node server
// Vite dev server proxy config for API + WS to the HTTPS backend.
// IMPORTANT: The Node server runs with TLS enabled, so targets MUST be https/wss.
// Using plain http/ws here against an HTTPS server causes connection resets and
// manifests as rapid connect/disconnect loops in the dashboard.
// secure:false allows self‑signed / local certs during development.
export default {
  '/api': {
    target: 'https://localhost:8443',
    changeOrigin: true,
    secure: true,
  },
  '/ws': {
    target: 'wss://localhost:8443',
    ws: true,
    changeOrigin: true,
    secure: true,
  }
};
