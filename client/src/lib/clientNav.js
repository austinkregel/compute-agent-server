/**
 * Client navigation helpers for dashboard routing.
 * These are pure functions for calculating target routes when switching clients.
 */

/**
 * Compute the target path when switching to a different client.
 * Preserves the current sub-page (e.g., /actions, /logs, /backups) if on a client route.
 * 
 * @param {object} opts
 * @param {string} opts.currentPath - Current route path (e.g., '/client/abc/actions')
 * @param {string} opts.nextClientId - The client ID to switch to
 * @returns {string} Target path (e.g., '/client/xyz/actions')
 */
export function computeClientTargetPath({ currentPath, nextClientId }) {
  if (!nextClientId) {
    return '/';
  }
  
  const encodedId = encodeURIComponent(nextClientId);
  const base = `/client/${encodedId}`;
  
  // If we're already on a client route, preserve the sub-page
  const onClientRoute = String(currentPath || '').startsWith('/client/');
  if (onClientRoute) {
    // Replace the existing /client/<id> segment with the new one
    return currentPath.replace(/\/client\/[^/]+/, base);
  }
  
  // Otherwise just go to the client's dashboard
  return base;
}

/**
 * Check if the keyboard event should trigger the command palette.
 * Returns true for Ctrl+K (Win/Linux) or Cmd+K (Mac).
 * Returns false if focus is in an input/textarea/contenteditable.
 * 
 * @param {KeyboardEvent} event
 * @returns {boolean}
 */
export function shouldOpenPalette(event) {
  // Check for K key with Ctrl or Meta (Cmd on Mac)
  if (event.key !== 'k' && event.key !== 'K') {
    return false;
  }
  
  const isMac = typeof navigator !== 'undefined' && /Mac|iPod|iPhone|iPad/.test(navigator.platform);
  const modifierPressed = isMac ? event.metaKey : event.ctrlKey;
  
  if (!modifierPressed) {
    return false;
  }
  
  // Don't trigger when typing in form elements
  const target = event.target;
  if (target instanceof HTMLElement) {
    const tagName = target.tagName.toLowerCase();
    if (tagName === 'input' || tagName === 'textarea' || tagName === 'select') {
      return false;
    }
    // Check for contenteditable - can be a boolean property or string attribute
    if (target.isContentEditable || target.contentEditable === 'true') {
      return false;
    }
  }
  
  return true;
}
