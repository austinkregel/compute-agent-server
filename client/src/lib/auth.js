/**
 * Authentication state management for Vue dashboard
 */

import { ref, reactive } from 'vue';

// Reactive auth state
export const isAuthenticated = ref(false);
// Whether the current session passes the server's admin role gate (OIDC group).
// Drives visibility of admin-only routes/nav (e.g. exec-allowlist management).
export const isAdmin = ref(false);
export const user = reactive({
  email: null,
  name: null,
  picture: null,
  sub: null,
});

let authCheckInterval = null;

/**
 * Check authentication status from server
 * OIDC is now REQUIRED - always returns false if not authenticated
 */
export async function checkAuth() {
  try {
    const res = await fetch('/api/auth/status', {
      credentials: 'include', // Include cookies
      headers: { 'Accept': 'application/json' },
    });

    if (!res.ok) {
      isAuthenticated.value = false;
      isAdmin.value = false;
      Object.assign(user, { email: null, name: null, picture: null, sub: null });
      return false;
    }

    const data = await res.json();
    isAuthenticated.value = data.authenticated || false;
    isAdmin.value = data.isAdmin || false;

    if (data.user) {
      Object.assign(user, {
        email: data.user.email || null,
        name: data.user.name || null,
        picture: data.user.picture || null,
        sub: data.user.sub || null,
      });
    } else {
      Object.assign(user, { email: null, name: null, picture: null, sub: null });
    }

    return isAuthenticated.value;
  } catch (err) {
    // Network error - assume not authenticated
    console.error('Auth check failed:', err);
    isAuthenticated.value = false;
    isAdmin.value = false;
    Object.assign(user, { email: null, name: null, picture: null, sub: null });
    return false;
  }
}

/**
 * Initiate login (redirect to OIDC provider)
 */
export function login() {
  window.location.href = '/auth/login';
}

/**
 * Logout
 */
export function logout() {
  window.location.href = '/auth/logout';
}

/**
 * Start periodic auth checks (optional, for session expiry)
 */
export function startAuthPolling(intervalMs = 60000) {
  if (authCheckInterval) {
    clearInterval(authCheckInterval);
  }
  authCheckInterval = setInterval(checkAuth, intervalMs);
}

/**
 * Stop auth polling
 */
export function stopAuthPolling() {
  if (authCheckInterval) {
    clearInterval(authCheckInterval);
    authCheckInterval = null;
  }
}

// Auto-check auth on module load
if (typeof window !== 'undefined') {
  checkAuth();
}









