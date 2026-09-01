import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  connected, clientIds, statsMap, on, send, __testInjectMessage,
  capabilitiesMap, clientHasCapability, clientCapabilityFeatures,
  smsThreadsMap, smsMessagesMap, fetchSmsThreads, fetchSmsMessages, sendSms,
  statsHistory,
} from './sharedWS.js';

// We will mock WebSocket to control behavior

// We don't rely on a real WebSocket here; we inject messages directly via helper.

describe('sharedWS connect-time replay', () => {
  it('replaces statsHistory from a stats_history replay rather than appending', () => {
    // Simulate a localStorage-restored series from a previous page load.
    statsHistory['replay-1'] = [{ cpu: 1 }, { cpu: 2 }];

    __testInjectMessage({
      type: 'stats_history',
      clientId: 'replay-1',
      samples: [{ cpu: 40 }, { cpu: 50 }],
    });

    // Replaced wholesale: a stale local series must not sit alongside the
    // server's authoritative one.
    expect(statsHistory['replay-1']).toEqual([{ cpu: 40 }, { cpu: 50 }]);
  });

  it('ignores a stats_history message with no samples array', () => {
    statsHistory['replay-2'] = [{ cpu: 7 }];
    __testInjectMessage({ type: 'stats_history', clientId: 'replay-2' });
    expect(statsHistory['replay-2']).toEqual([{ cpu: 7 }]);
  });
});

describe('sharedWS basic reactive state', () => {
  it('updates clientIds on client_list', async () => {
    __testInjectMessage({ type: 'client_list', clientIds: ['x','y'] });
    expect(clientIds.value).toEqual(['x','y']);
  });

  it('emits and updates state', async () => {
    // Create a new instance by importing (already imported). Force a fake message
    __testInjectMessage({ type:'client_list', clientIds:['a','b'] });
    expect(clientIds.value).toEqual(['a','b']);
    __testInjectMessage({ type:'stats', clientId:'a', data:{ cpu:10 } });
    expect(statsMap['a'].cpu).toBe(10);
  });

  it('populates capabilitiesMap from stats.capabilities', () => {
    __testInjectMessage({ type: 'client_list', clientIds: ['phone-1'] });
    __testInjectMessage({
      type: 'stats',
      clientId: 'phone-1',
      data: {
        cpu: 5,
        capabilities: {
          docker: { state: 'unavailable', detail: 'disabled by config' },
          telephony: { state: 'enabled', features: ['sms', 'volte_bridge'] },
        },
      },
    });

    expect(capabilitiesMap['phone-1'].telephony.state).toBe('enabled');
    expect(clientHasCapability('phone-1', 'telephony')).toBe(true);
    expect(clientHasCapability('phone-1', 'docker')).toBe(false);
    expect(clientHasCapability('phone-1', 'docker', 'available')).toBe(false);
    expect(clientCapabilityFeatures('phone-1', 'telephony')).toEqual(['sms', 'volte_bridge']);
    expect(clientCapabilityFeatures('phone-1', 'docker')).toEqual([]);
    expect(clientHasCapability('unknown-client', 'telephony')).toBe(false);
  });

  it('send() safely ignores if socket not open', () => {
    // Replace ws with closed mock
    // Temporarily simulate closed socket environment
    const original = global.WebSocket;
    class ClosedWS { constructor(){ this.readyState = 3; } send(){} }
    global.WebSocket = ClosedWS;
    // Should not throw
    send({ test: true });
    global.WebSocket = original;
  });
});

describe('sharedWS SMS helpers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fetchSmsThreads populates smsThreadsMap on success', async () => {
    const threads = [{ threadId: 1, address: '+15551234', snippet: 'hi', unreadCount: 1 }];
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ threads }),
    })));

    const result = await fetchSmsThreads('phone-1');
    expect(result).toEqual(threads);
    expect(smsThreadsMap['phone-1']).toEqual(threads);
    expect(fetch).toHaveBeenCalledWith(
      '/api/client/phone-1/sms/threads',
      expect.objectContaining({ credentials: 'include' })
    );
  });

  it('fetchSmsThreads returns [] on a non-ok response without throwing', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false })));
    const result = await fetchSmsThreads('phone-1');
    expect(result).toEqual([]);
  });

  it('fetchSmsMessages populates smsMessagesMap keyed by client:thread', async () => {
    const messages = [{ messageId: 1, body: 'hey', direction: 'in' }];
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ messages }),
    })));

    const result = await fetchSmsMessages('phone-1', '42');
    expect(result).toEqual(messages);
    expect(smsMessagesMap['phone-1:42']).toEqual(messages);
    expect(fetch).toHaveBeenCalledWith(
      '/api/client/phone-1/sms/threads/42/messages',
      expect.objectContaining({ credentials: 'include' })
    );
  });

  it('sendSms POSTs to/body and returns the parsed response on success', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ status: 'sent', messageId: 'm1', threadId: 1 }),
    }));
    vi.stubGlobal('fetch', fetchMock);

    const result = await sendSms('phone-1', '+15551234', 'hello');
    expect(result).toEqual({ status: 'sent', messageId: 'm1', threadId: 1 });

    const [url, opts] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/client/phone-1/sms/send');
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ to: '+15551234', body: 'hello' });
  });

  it('sendSms surfaces the server error message on a non-ok response', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: false,
      status: 502,
      json: async () => ({ error: 'companion not connected' }),
    })));

    const result = await sendSms('phone-1', '+15551234', 'hello');
    expect(result).toEqual({ error: 'companion not connected' });
  });

  it('sendSms returns an error object on a network failure instead of throwing', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('offline'); }));
    const result = await sendSms('phone-1', '+15551234', 'hello');
    expect(result.error).toBe('offline');
  });

  it('sms_received triggers a threads refresh for the matching client', async () => {
    const threads = [{ threadId: 1, address: '+15551234', snippet: 'new message', unreadCount: 1 }];
    const fetchMock = vi.fn(async () => ({ ok: true, json: async () => ({ threads }) }));
    vi.stubGlobal('fetch', fetchMock);

    __testInjectMessage({ type: 'sms_received', clientId: 'phone-1', address: '+15551234', body: 'new message', ts: Date.now() });

    // fetchSmsThreads is async; give its promise a tick to resolve.
    await new Promise(resolve => setTimeout(resolve, 0));

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/client/phone-1/sms/threads',
      expect.objectContaining({ credentials: 'include' })
    );
    expect(smsThreadsMap['phone-1']).toEqual(threads);
  });
});
