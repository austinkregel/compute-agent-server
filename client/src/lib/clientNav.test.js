import { describe, it, expect } from 'vitest';
import { computeClientTargetPath, shouldOpenPalette } from './clientNav.js';

describe('computeClientTargetPath', () => {
  it('returns root when nextClientId is empty', () => {
    expect(computeClientTargetPath({ currentPath: '/client/abc', nextClientId: '' })).toBe('/');
    expect(computeClientTargetPath({ currentPath: '/', nextClientId: '' })).toBe('/');
    expect(computeClientTargetPath({ currentPath: '/client/abc/actions', nextClientId: null })).toBe('/');
  });

  it('navigates from root to client dashboard', () => {
    expect(computeClientTargetPath({ currentPath: '/', nextClientId: 'xyz' })).toBe('/client/xyz');
  });

  it('switches client on dashboard route', () => {
    expect(computeClientTargetPath({ currentPath: '/client/abc', nextClientId: 'xyz' })).toBe('/client/xyz');
  });

  it('preserves /actions subpage when switching clients', () => {
    expect(computeClientTargetPath({ currentPath: '/client/abc/actions', nextClientId: 'xyz' })).toBe('/client/xyz/actions');
  });

  it('preserves /logs subpage when switching clients', () => {
    expect(computeClientTargetPath({ currentPath: '/client/abc/logs', nextClientId: 'xyz' })).toBe('/client/xyz/logs');
  });

  it('preserves /backups subpage when switching clients', () => {
    expect(computeClientTargetPath({ currentPath: '/client/old-client/backups', nextClientId: 'new-client' })).toBe('/client/new-client/backups');
  });

  it('URL-encodes special characters in clientId', () => {
    expect(computeClientTargetPath({ currentPath: '/', nextClientId: 'client with spaces' })).toBe('/client/client%20with%20spaces');
    expect(computeClientTargetPath({ currentPath: '/', nextClientId: 'client/slash' })).toBe('/client/client%2Fslash');
    expect(computeClientTargetPath({ currentPath: '/client/a', nextClientId: 'b&c=d' })).toBe('/client/b%26c%3Dd');
  });

  it('handles undefined/null currentPath gracefully', () => {
    expect(computeClientTargetPath({ currentPath: undefined, nextClientId: 'abc' })).toBe('/client/abc');
    expect(computeClientTargetPath({ currentPath: null, nextClientId: 'abc' })).toBe('/client/abc');
  });
});

describe('shouldOpenPalette', () => {
  function createKeyboardEvent(key, options = {}) {
    return {
      key,
      ctrlKey: options.ctrlKey || false,
      metaKey: options.metaKey || false,
      target: options.target || document.createElement('div'),
      preventDefault: () => {},
    };
  }

  it('returns true for Ctrl+K on non-Mac', () => {
    // Mock non-Mac platform
    const originalNavigator = global.navigator;
    Object.defineProperty(global, 'navigator', {
      value: { platform: 'Win32' },
      writable: true,
    });
    
    const event = createKeyboardEvent('k', { ctrlKey: true });
    expect(shouldOpenPalette(event)).toBe(true);
    
    global.navigator = originalNavigator;
  });

  it('returns true for Cmd+K on Mac', () => {
    const originalNavigator = global.navigator;
    Object.defineProperty(global, 'navigator', {
      value: { platform: 'MacIntel' },
      writable: true,
    });
    
    const event = createKeyboardEvent('k', { metaKey: true });
    expect(shouldOpenPalette(event)).toBe(true);
    
    global.navigator = originalNavigator;
  });

  it('returns false for K without modifier', () => {
    const event = createKeyboardEvent('k');
    expect(shouldOpenPalette(event)).toBe(false);
  });

  it('returns false for other keys with Ctrl', () => {
    const event = createKeyboardEvent('a', { ctrlKey: true });
    expect(shouldOpenPalette(event)).toBe(false);
  });

  it('returns false when target is input element', () => {
    const originalNavigator = global.navigator;
    Object.defineProperty(global, 'navigator', {
      value: { platform: 'Win32' },
      writable: true,
    });
    
    const input = document.createElement('input');
    const event = createKeyboardEvent('k', { ctrlKey: true, target: input });
    expect(shouldOpenPalette(event)).toBe(false);
    
    global.navigator = originalNavigator;
  });

  it('returns false when target is textarea element', () => {
    const originalNavigator = global.navigator;
    Object.defineProperty(global, 'navigator', {
      value: { platform: 'Win32' },
      writable: true,
    });
    
    const textarea = document.createElement('textarea');
    const event = createKeyboardEvent('k', { ctrlKey: true, target: textarea });
    expect(shouldOpenPalette(event)).toBe(false);
    
    global.navigator = originalNavigator;
  });

  it('returns false when target is contenteditable', () => {
    const originalNavigator = global.navigator;
    Object.defineProperty(global, 'navigator', {
      value: { platform: 'Win32' },
      writable: true,
    });
    
    const div = document.createElement('div');
    div.contentEditable = 'true';
    const event = createKeyboardEvent('k', { ctrlKey: true, target: div });
    expect(shouldOpenPalette(event)).toBe(false);
    
    global.navigator = originalNavigator;
  });
});
