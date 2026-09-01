import { ref, reactive } from 'vue';

// Generic lightweight dispatcher factory so non-dashboard contexts (e.g. headless agent)
// can reuse the same event subscription pattern without copying code.
export function createDispatcher() {
  const listeners = new Map();
  function on(type, handler) {
    if (!listeners.has(type)) listeners.set(type, []);
    listeners.get(type).push(handler);
    return () => {
      const arr = listeners.get(type) || [];
      const idx = arr.indexOf(handler);
      if (idx !== -1) arr.splice(idx, 1);
    };
  }
  function emit(type, payload) {
    const arr = listeners.get(type);
    if (arr) arr.forEach(fn => { try { fn(payload); } catch {} });
    const any = listeners.get('*');
    if (any) any.forEach(fn => { try { fn({ type, ...payload }); } catch {} });
  }
  return { on, emit };
}

const listeners = new Map();

// Reactive state exposed to any component that imports this module
export const connected = ref(false);
export const clientIds = ref([]);
export const statsMap = reactive({}); // clientId -> latest stats snapshot
// Per-client update results history (agent self-update attempts)
// clientId -> [{ ts, ok, tag, error, detail, repo }]
export const agentUpdateHistory = reactive({});
// Per-client ring buffer of recent stats (up to HISTORY_LIMIT)
export const statsHistory = reactive({}); // clientId -> array newest-last
const HISTORY_LIMIT = 15;
const UPDATE_HISTORY_LIMIT = 50;
// Active per-client log tail state (optional; used by LogsViewer)
export const logTailState = reactive({}); // clientId -> { session, running, lastTs }
// Per-client kiosk status (optional; used by QuickActions for kiosk mode control)
// clientId -> { running, connected, content: { kind, title?, text?, url? }, lastError, ts }
export const kioskStatusMap = reactive({});
// Per-client variant status (headless vs kiosk binary)
// clientId -> { current, desired, kioskAvailable, lastSwitchError, lastSwitchAttempt, ts }
export const variantStatusMap = reactive({});
// Per-client kiosk layouts (populated on demand via kiosk_get_layouts)
// clientId -> { layouts: { name: { cols, rows, widgets } } }
export const kioskLayoutsMap = reactive({});

// Per-client Docker/Swarm status (extracted from stats.docker)
// clientId -> { available, version, swarm: {...}, containers: {...} }
export const dockerStatusMap = reactive({});

// Per-client capability-registry snapshot (extracted from stats.capabilities).
// clientId -> { [name]: { state: 'unavailable'|'available'|'enabled', detail?, features?, meta? } }
// This is the generic discovery signal — "is X supported at all" — that new
// features (and eventually Docker/battery) should gate on instead of each
// widget independently sniffing for its own data field.
export const capabilitiesMap = reactive({});

// Returns true if clientId has reported the named capability at or above the
// given minimum state ('available' or 'enabled', default 'enabled').
export function clientHasCapability(clientId, name, min = 'enabled') {
  const info = capabilitiesMap[clientId]?.[name];
  if (!info) return false;
  if (min === 'available') return info.state === 'available' || info.state === 'enabled';
  return info.state === 'enabled';
}

// Returns the feature list for a capability (e.g. ['sms', 'volte_bridge']), or
// an empty array if the capability isn't reported.
export function clientCapabilityFeatures(clientId, name) {
  return capabilitiesMap[clientId]?.[name]?.features || [];
}

// Per-client OS alerts snapshot
// clientId -> { alerts: [...], since, totalCount, hasCritical, lastScanTime }
export const alertsMap = reactive({});

// Per-client SMS conversation threads (from GET /api/client/:id/sms/threads)
// clientId -> [{ threadId, address, displayName?, snippet, unreadCount, lastMessageAt }]
export const smsThreadsMap = reactive({});
// Per-(client, thread) message history
// `${clientId}:${threadId}` -> [{ messageId, threadId, address, direction, body, status, timestamp }]
export const smsMessagesMap = reactive({});
// Per-client ring buffer of recent critical/error OS alerts (for notifications/toast)
// clientId -> [{ id, ts, severity, category, message, source, count }]
export const osAlertHistory = reactive({});
const ALERT_HISTORY_LIMIT = 20;

function loadPersistedHistory() {
  try {
    const raw = localStorage.getItem('dashboard.statsHistory');
    if (!raw) return;
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === 'object') {
      for (const [cid, arr] of Object.entries(parsed)) {
        if (Array.isArray(arr)) statsHistory[cid] = arr.slice(-HISTORY_LIMIT);
      }
    }
  } catch { /* ignore */ }
}
function persistHistorySoon() {
  // Debounce writes to avoid spamming localStorage (only in browser)
  if (typeof localStorage === 'undefined') return;
  if (persistHistorySoon._timer) clearTimeout(persistHistorySoon._timer);
  persistHistorySoon._timer = setTimeout(() => {
    try {
      const toStore = {};
      for (const [cid, arr] of Object.entries(statsHistory)) {
        toStore[cid] = arr.slice(-HISTORY_LIMIT);
      }
      localStorage.setItem('dashboard.statsHistory', JSON.stringify(toStore));
    } catch { /* ignore */ }
  }, 500);
}

if (typeof window !== 'undefined') loadPersistedHistory();

// Internal listeners keyed by message type; '*' for all
function emit(type, payload) {
  const arr = listeners.get(type);
  if (arr) arr.forEach(fn => { try { fn(payload); } catch {} });
  const any = listeners.get('*');
  if (any) any.forEach(fn => { try { fn({ type, ...payload }); } catch {} });
}

export function on(type, handler) {
  if (!listeners.has(type)) listeners.set(type, []);
  listeners.get(type).push(handler);
  return () => {
    const list = listeners.get(type) || [];
    const idx = list.indexOf(handler);
    if (idx !== -1) list.splice(idx, 1);
  };
}

export function off(type, handler) {
  const list = listeners.get(type) || [];
  const idx = list.indexOf(handler);
  if (idx !== -1) list.splice(idx, 1);
}

let ws;
let reconnectTimer;
let manualClose = false;
let reconnectDelay = 1000;
const RECONNECT_MAX = 30000;

function handleMessage(obj) {
  switch (obj.type) {
    case 'client_list':
      clientIds.value = obj.clientIds || [];
      break;
    case 'stats':
      if (obj.clientId && obj.data) {
        statsMap[obj.clientId] = obj.data;
        if (obj.data.docker) {
          dockerStatusMap[obj.clientId] = obj.data.docker;
        }
        if (obj.data.capabilities) {
          capabilitiesMap[obj.clientId] = obj.data.capabilities;
        }
        const arr = statsHistory[obj.clientId] || (statsHistory[obj.clientId] = []);
        arr.push(obj.data);
        while (arr.length > HISTORY_LIMIT) arr.shift();
        persistHistorySoon();
      }
      break;
    case 'stats_history':
      // Authoritative history replayed by the server when this dashboard
      // connects. Replaces the localStorage-restored ring buffer rather than
      // appending to it, so sparklines can't show a stale local series next to
      // fresh server values.
      if (obj.clientId && Array.isArray(obj.samples)) {
        statsHistory[obj.clientId] = obj.samples.slice(-HISTORY_LIMIT);
        persistHistorySoon();
      }
      break;
    case 'ping':
      // Server keepalive, reply with pong
      send({ type: 'pong', ts: Date.now() });
      break;
    case 'pong':
      // Ignore or could record latency
      break;
    case 'agent_update_result':
      if (obj.clientId) {
        const arr = agentUpdateHistory[obj.clientId] || (agentUpdateHistory[obj.clientId] = []);
        arr.push({
          ts: obj.ts || new Date().toISOString(),
          ok: !!obj.ok,
          tag: obj.tag || '',
          repo: obj.repo || '',
          error: obj.error || '',
          detail: obj.detail || ''
        });
        while (arr.length > UPDATE_HISTORY_LIMIT) arr.shift();
      }
      break;
    case 'log_tail_started':
      if (obj.clientId && obj.session) {
        logTailState[obj.clientId] = {
          session: obj.session,
          running: true,
          lastTs: obj.ts || new Date().toISOString()
        };
      }
      break;
    case 'log_tail_closed':
      if (obj.clientId && obj.session) {
        const st = logTailState[obj.clientId];
        if (st && st.session === obj.session) {
          logTailState[obj.clientId] = { ...st, running: false, lastTs: obj.ts || new Date().toISOString() };
        }
      }
      break;
    case 'kiosk_status':
      if (obj.clientId && obj.kiosk) {
        kioskStatusMap[obj.clientId] = {
          running: !!obj.kiosk.running,
          connected: !!obj.kiosk.connected,
          content: obj.kiosk.content || { kind: 'blank' },
          lastError: obj.kiosk.lastError || '',
          ts: obj.kiosk.ts || obj.ts || new Date().toISOString()
        };
      }
      break;
    case 'variant_status':
      if (obj.clientId) {
        variantStatusMap[obj.clientId] = {
          current: obj.current || 'headless',
          desired: obj.desired || 'headless',
          kioskAvailable: !!obj.kioskAvailable,
          lastSwitchError: obj.lastSwitchError || '',
          lastSwitchAttempt: obj.lastSwitchAttempt || '',
          ts: obj.ts || new Date().toISOString()
        };
      }
      break;
    case 'variant_switch_result':
      if (obj.clientId) {
        // Update status on switch result
        const existing = variantStatusMap[obj.clientId] || {};
        if (obj.ok) {
          variantStatusMap[obj.clientId] = {
            ...existing,
            current: obj.variant || existing.current,
            lastSwitchError: '',
            lastSwitchAttempt: obj.ts || new Date().toISOString(),
            ts: obj.ts || new Date().toISOString()
          };
        } else {
          variantStatusMap[obj.clientId] = {
            ...existing,
            lastSwitchError: obj.error || obj.detail || 'switch failed',
            lastSwitchAttempt: obj.ts || new Date().toISOString(),
            ts: obj.ts || new Date().toISOString()
          };
        }
      }
      break;
    case 'sms_received':
      // Incoming SMS pushed live from the phone agent. The push doesn't carry
      // a threadId (only the server-side DB assigns one), so refresh the
      // thread list rather than guessing which entry to patch; components
      // showing an open conversation subscribe via on('sms_received', ...)
      // to refetch that thread's messages themselves.
      if (obj.clientId) {
        fetchSmsThreads(obj.clientId);
      }
      break;
    case 'alerts':
      // OS alerts snapshot from server
      if (obj.clientId && obj.data) {
        alertsMap[obj.clientId] = {
          alerts: obj.data.alerts || [],
          since: obj.data.since || '',
          totalCount: obj.data.totalCount || 0,
          hasCritical: !!obj.data.hasCritical,
          lastScanTime: obj.data.lastScanTime || new Date().toISOString()
        };
      }
      break;
    case 'kiosk_layout_saved':
      // Ack from agent after saving a layout; just emit for listener
      break;
    case 'kiosk_layouts':
      if (obj.clientId && obj.layouts) {
        kioskLayoutsMap[obj.clientId] = obj.layouts;
      }
      break;
    case 'os_alert':
      // Individual OS alert notification (for critical alerts)
      if (obj.clientId && obj.alert) {
        const arr = osAlertHistory[obj.clientId] || (osAlertHistory[obj.clientId] = []);
        // Avoid duplicates by ID
        if (!arr.find(a => a.id === obj.alert.id)) {
          arr.push({
            ...obj.alert,
            receivedAt: new Date().toISOString()
          });
          while (arr.length > ALERT_HISTORY_LIMIT) arr.shift();
        }
      }
      break;
  }
  emit(obj.type, obj);
}

// Fetch the last persisted stats snapshot from the server (REST) and seed maps if we have nothing yet.
async function fetchLatestStats(clientId) {
  if (!clientId) return null;
  try {
    const res = await fetch(`/api/client/${encodeURIComponent(clientId)}/stats`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return null;
    const json = await res.json();
    if (json && json.stats && !statsMap[clientId]) {
      statsMap[clientId] = json.stats;
      const arr = statsHistory[clientId] || (statsHistory[clientId] = []);
      // Prevent duplicate if same ts already present as first (unlikely on first load)
      const ts = json.stats.ts;
      if (!arr.find(s => s.ts === ts)) {
        arr.push(json.stats);
        while (arr.length > HISTORY_LIMIT) arr.shift();
      }
      persistHistorySoon();
      emit('stats', { type: 'stats', clientId, data: json.stats, hydrated: true });
    }
    return json;
  } catch { return null; }
}

// Public helper: ensure stats present for a client (load from REST if absent)
export async function ensureClientStatsLoaded(clientId) {
  if (!clientId) return;
  if (statsMap[clientId]) return; // already have something
  await fetchLatestStats(clientId);
}

// Fetch alerts for a client from the server REST API
async function fetchAlerts(clientId) {
  if (!clientId) return null;
  try {
    const res = await fetch(`/api/client/${encodeURIComponent(clientId)}/alerts`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return null;
    const json = await res.json();
    if (json && !alertsMap[clientId]) {
      alertsMap[clientId] = {
        alerts: json.alerts || [],
        since: json.since || '',
        totalCount: json.totalCount || 0,
        hasCritical: !!json.hasCritical,
        lastScanTime: json.lastScanTime || new Date().toISOString()
      };
      emit('alerts', { type: 'alerts', clientId, data: alertsMap[clientId], hydrated: true });
    }
    return json;
  } catch { return null; }
}

// Public helper: ensure alerts present for a client (load from REST if absent)
export async function ensureClientAlertsLoaded(clientId) {
  if (!clientId) return;
  if (alertsMap[clientId]) return; // already have something
  await fetchAlerts(clientId);
}

// Fetch Docker status for a client from REST API
export async function fetchClientDocker(clientId) {
  if (!clientId) return null;
  try {
    const res = await fetch(`/api/client/${encodeURIComponent(clientId)}/docker`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return null;
    const json = await res.json();
    if (json && json.docker) {
      dockerStatusMap[clientId] = json.docker;
    }
    return json;
  } catch { return null; }
}

// Fetch SMS conversation threads for a client from REST API
export async function fetchSmsThreads(clientId) {
  if (!clientId) return [];
  try {
    const res = await fetch(`/api/client/${encodeURIComponent(clientId)}/sms/threads`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return [];
    const json = await res.json();
    const threads = json.threads || [];
    smsThreadsMap[clientId] = threads;
    return threads;
  } catch { return []; }
}

// Fetch messages within one SMS thread from REST API
export async function fetchSmsMessages(clientId, threadId) {
  if (!clientId || !threadId) return [];
  try {
    const res = await fetch(
      `/api/client/${encodeURIComponent(clientId)}/sms/threads/${encodeURIComponent(threadId)}/messages`,
      { credentials: 'include', headers: { 'Accept': 'application/json' } }
    );
    if (!res.ok) return [];
    const json = await res.json();
    const messages = json.messages || [];
    smsMessagesMap[`${clientId}:${threadId}`] = messages;
    return messages;
  } catch { return []; }
}

// Send an SMS via a client's companion app. Returns the parsed response body
// (either the send result or an { error } object) — callers check for .error.
export async function sendSms(clientId, to, body) {
  try {
    const res = await fetch(`/api/client/${encodeURIComponent(clientId)}/sms/send`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
      body: JSON.stringify({ to, body })
    });
    const json = await res.json().catch(() => ({}));
    if (!res.ok) return { error: json.error || `send failed (${res.status})` };
    return json;
  } catch (e) {
    return { error: e?.message || 'network error' };
  }
}

// Fetch swarm cluster groupings from REST API
export async function fetchSwarmClusters() {
  try {
    const res = await fetch('/api/swarm/clusters', {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return [];
    const json = await res.json();
    return json.clusters || [];
  } catch { return []; }
}

// --- Stack API helpers ---

export async function fetchStacks() {
  try {
    const res = await fetch('/api/stacks', {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return [];
    const json = await res.json();
    return json.stacks || [];
  } catch { return []; }
}

export async function fetchStack(stackId) {
  try {
    const res = await fetch(`/api/stacks/${stackId}`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return null;
    return await res.json();
  } catch { return null; }
}

export async function createStack(data) {
  const res = await fetch('/api/stacks', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
    body: JSON.stringify(data)
  });
  return await res.json();
}

export async function updateStack(stackId, data) {
  const res = await fetch(`/api/stacks/${stackId}`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
    body: JSON.stringify(data)
  });
  return await res.json();
}

export async function deleteStack(stackId) {
  const res = await fetch(`/api/stacks/${stackId}`, {
    method: 'DELETE',
    credentials: 'include',
    headers: { 'Accept': 'application/json' }
  });
  return await res.json();
}

export async function fetchStackVersions(stackId) {
  try {
    const res = await fetch(`/api/stacks/${stackId}/versions`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return [];
    const json = await res.json();
    return json.versions || [];
  } catch { return []; }
}

export async function importComposeScan(clientId, directory) {
  const res = await fetch('/api/stacks/import/scan', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
    body: JSON.stringify({ clientId, directory })
  });
  return await res.json();
}

export async function importComposeParse(clientId, selections) {
  const res = await fetch('/api/stacks/import', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
    body: JSON.stringify({ clientId, selections })
  });
  return await res.json();
}

export async function fetchEnvGroups() {
  try {
    const res = await fetch('/api/env-groups', {
      credentials: 'include',
      headers: { 'Accept': 'application/json' }
    });
    if (!res.ok) return [];
    const json = await res.json();
    return json.envGroups || [];
  } catch { return []; }
}

export async function deployStack(stackId) {
  const res = await fetch(`/api/stacks/${stackId}/deploy`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  });
  return res.json();
}

export async function stopStack(stackId) {
  const res = await fetch(`/api/stacks/${stackId}/stop`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  });
  return res.json();
}

export async function restartStack(stackId) {
  const res = await fetch(`/api/stacks/${stackId}/restart`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' }
  });
  return res.json();
}

export function send(obj) {
  try {
    if (ws && ws.readyState === WebSocket.OPEN) {
      const { type, ...rest } = obj;
      ws.send(JSON.stringify({ event: type || 'message', data: rest }));
    }
  } catch {}
}

function scheduleReconnect() {
  if (manualClose) return;
  if (reconnectTimer) clearTimeout(reconnectTimer);
  reconnectTimer = setTimeout(() => {
    connect();
    reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX);
  }, reconnectDelay);
}

export function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return ws;
  manualClose = false;

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const wsUrl = `${proto}//${location.host}/ws/dashboard`;

  ws = new WebSocket(wsUrl);

  ws.onopen = () => {
    connected.value = true;
    reconnectDelay = 1000; // reset backoff on successful connection
    emit('open', {});
  };

  ws.onclose = (ev) => {
    connected.value = false;
    emit('close', { reason: ev.reason || 'closed', code: ev.code });
    scheduleReconnect();
  };

  ws.onerror = () => {
    // error event is always followed by close, reconnection handled there
  };

  ws.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data);
      if (!msg || !msg.event) return;
      // Map the { event, data } envelope to the { type, ...fields } format
      // that handleMessage expects.
      handleMessage({ type: msg.event, ...(msg.data || {}) });
    } catch { /* ignore malformed messages */ }
  };

  return ws;
}

export function close() {
  manualClose = true;
  if (reconnectTimer) clearTimeout(reconnectTimer);
  if (ws) try { ws.close(); } catch {}
}

// Auto-connect on first import in browser context
if (typeof window !== 'undefined') {
  connect();
  window.__sharedDashboardWS = { connect, send, state: { connected, clientIds, statsMap }, _ws: () => ws };
}

// --- Test helper (no effect in production usage) ---
// Allows unit tests to inject a fake inbound dashboard message without a real socket.
export function __testInjectMessage(obj) { handleMessage(obj); }
