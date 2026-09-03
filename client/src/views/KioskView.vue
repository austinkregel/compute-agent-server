<script setup>
import { ref, computed, reactive, onMounted, onUnmounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import { send, on as onWS, kioskStatusMap, variantStatusMap, kioskLayoutsMap } from '../lib/sharedWS.js';
import { isAdmin } from '../lib/auth.js';

const route = useRoute();
const clientId = computed(() => String(route.params.clientId || ''));

const WIDGET_TYPES = [
  { id: 'stats-primary', label: 'System Stats', icon: '📊' },
  { id: 'stats-secondary', label: 'Disk / Network', icon: '💾' },
  { id: 'cpu', label: 'CPU', icon: '🖥' },
  { id: 'memory', label: 'Memory', icon: '🧠' },
  { id: 'battery', label: 'Battery', icon: '🔋' },
  { id: 'disk', label: 'Disk', icon: '💽' },
  { id: 'network', label: 'Network', icon: '🌐' },
  { id: 'system-health', label: 'System Health', icon: '❤️' },
  { id: 'weather-current', label: 'Weather', icon: '🌤' },
  { id: 'weather-forecast', label: '7-Day Forecast', icon: '📅' },
  { id: 'clock-calendar', label: 'Clock / Calendar', icon: '🕐' },
  { id: 'news', label: 'News', icon: '📰' },
  { id: 'crypto', label: 'Crypto', icon: '₿' },
  { id: 'iss-tracker', label: 'ISS Tracker', icon: '🛰' },
  { id: 'astronomy', label: 'Astronomy', icon: '🌙' },
  { id: 'world-clocks', label: 'World Clocks', icon: '🌍' },
  { id: 'ambient-photo', label: 'Ambient Photo', icon: '🖼' },
];

const PRESETS = [
  { name: 'system', label: 'System', desc: 'Host telemetry', cols: 3, rows: 2 },
  { name: 'ultrawide', label: 'Ultrawide', desc: '5120×1440', cols: 5, rows: 3 },
  { name: 'wide', label: 'Wide', desc: '2560×1440', cols: 3, rows: 3 },
  { name: 'classic', label: 'Classic', desc: '4:3', cols: 2, rows: 3 },
];

// 'system' is also the agent's cold-start default (see kiosk.DefaultLayoutName),
// so opening the editor shows what an unconfigured kiosk is already displaying.
const currentPreset = ref('system');
const gridCols = ref(3);
const gridRows = ref(2);
const widgets = ref([]);
const units = ref('imperial');
const saving = ref(false);
const applying = ref(false);
// Last write failure reported by the server or agent. Save/Apply are
// admin-gated in the relay (kiosk_get_layouts is not), so a non-admin session
// loads the editor normally and has every write refused; surface that rather
// than letting the button report a success it never observed.
const writeError = ref('');
const writeOk = ref('');
const writeTimers = [];
// 'pending' = no reply from the agent yet, 'agent' = loaded from the agent's
// store, 'default' = agent has no such layout and this is a local default.
const layoutSource = ref('pending');
// Whether kioskLayoutsMap holds a real reply from this agent yet.
const layoutsLoaded = computed(() => !!kioskLayoutsMap[clientId.value]);
const showPalette = ref(false);
const showKioskMessageModal = ref(false);
const showKioskUrlModal = ref(false);
const kioskMessageTitle = ref('');
const kioskMessageText = ref('');
const kioskUrl = ref('');
const switchingVariant = ref(false);

// Drag state
const gridEl = ref(null);
const dragging = ref(false);
const dragWidgetIdx = ref(-1);
const dragIsResize = ref(false);
const dragGhostCol = ref(-1);
const dragGhostRow = ref(-1);
const dragGhostW = ref(1);
const dragGhostH = ref(1);
const dragGhostValid = ref(false);

// "Add from palette" mode
const addingType = ref('');

const kioskStatus = computed(() => clientId.value ? kioskStatusMap[clientId.value] || null : null);
const variantStatus = computed(() => clientId.value ? variantStatusMap[clientId.value] || null : null);
const isKioskActive = computed(() => {
  const vs = variantStatus.value;
  if (vs) return vs.current === 'kiosk';
  const ks = kioskStatus.value;
  return ks?.connected || ks?.running || false;
});
const kioskContentLabel = computed(() => {
  const k = kioskStatus.value?.content?.kind || 'blank';
  const labels = { blank: 'Blank', dashboard: 'Dashboard', message: 'Message', url: 'URL', page: 'Page' };
  const base = labels[k] || k;
  if (k === 'page' && kioskStatus.value?.content?.layout) return `Page: ${kioskStatus.value.content.layout}`;
  return base;
});

const isValidKioskUrl = computed(() => {
  const url = kioskUrl.value.trim();
  if (!url) return false;
  try { const u = new URL(url); return u.protocol === 'http:' || u.protocol === 'https:'; } catch { return false; }
});

function selectPreset(preset) {
  currentPreset.value = preset.name;
  gridCols.value = preset.cols;
  gridRows.value = preset.rows;
  loadLayoutFromStore(preset.name);
}

function loadLayoutFromStore(name) {
  const layouts = kioskLayoutsMap[clientId.value];
  if (layouts && layouts[name]) {
    const l = layouts[name];
    gridCols.value = l.cols || gridCols.value;
    gridRows.value = l.rows || gridRows.value;
    widgets.value = (l.widgets || []).map(w => ({ ...w }));
    if (l.units) units.value = l.units;
    layoutSource.value = 'agent';
    return;
  }
  // No layout of this name on the agent. Fall back to the built-in shape so the
  // editor is usable, but record that this is NOT the agent's state — otherwise
  // a client-side default is indistinguishable from persisted data.
  loadDefaultLayout(name);
  layoutSource.value = layouts ? 'default' : 'pending';
}

function loadDefaultLayout(name) {
  const defaults = {
    system: [
      { type: 'cpu', col: 1, row: 1, w: 1, h: 1 },
      { type: 'memory', col: 2, row: 1, w: 1, h: 1 },
      { type: 'battery', col: 3, row: 1, w: 1, h: 1 },
      { type: 'disk', col: 1, row: 2, w: 1, h: 1 },
      { type: 'network', col: 2, row: 2, w: 1, h: 1 },
      { type: 'system-health', col: 3, row: 2, w: 1, h: 1 },
    ],
    ultrawide: [
      { type: 'stats-primary', col: 1, row: 1, w: 1, h: 1 },
      { type: 'weather-current', col: 2, row: 1, w: 1, h: 1 },
      { type: 'clock-calendar', col: 3, row: 1, w: 1, h: 1 },
      { type: 'iss-tracker', col: 4, row: 1, w: 1, h: 1 },
      { type: 'crypto', col: 5, row: 1, w: 1, h: 1 },
      { type: 'stats-secondary', col: 1, row: 2, w: 1, h: 1 },
      { type: 'weather-forecast', col: 2, row: 2, w: 1, h: 1 },
      { type: 'news', col: 3, row: 2, w: 1, h: 1 },
      { type: 'astronomy', col: 4, row: 2, w: 1, h: 1 },
      { type: 'world-clocks', col: 5, row: 2, w: 1, h: 1 },
      { type: 'ambient-photo', col: 1, row: 3, w: 5, h: 1 },
    ],
    wide: [
      { type: 'stats-primary', col: 1, row: 1, w: 1, h: 1 },
      { type: 'clock-calendar', col: 2, row: 1, w: 1, h: 1 },
      { type: 'weather-current', col: 3, row: 1, w: 1, h: 1 },
      { type: 'news', col: 1, row: 2, w: 1, h: 1 },
      { type: 'crypto', col: 2, row: 2, w: 1, h: 1 },
      { type: 'world-clocks', col: 3, row: 2, w: 1, h: 1 },
      { type: 'stats-secondary', col: 1, row: 3, w: 1, h: 1 },
      { type: 'iss-tracker', col: 2, row: 3, w: 1, h: 1 },
      { type: 'astronomy', col: 3, row: 3, w: 1, h: 1 },
    ],
    classic: [
      { type: 'weather-current', col: 1, row: 1, w: 1, h: 1 },
      { type: 'stats-primary', col: 2, row: 1, w: 1, h: 1 },
      { type: 'news', col: 1, row: 2, w: 1, h: 1 },
      { type: 'crypto', col: 2, row: 2, w: 1, h: 1 },
      { type: 'world-clocks', col: 1, row: 3, w: 1, h: 1 },
      { type: 'iss-tracker', col: 2, row: 3, w: 1, h: 1 },
    ],
  };
  widgets.value = (defaults[name] || defaults.system).map(w => ({ ...w }));
}

function getWidgetLabel(type) {
  return WIDGET_TYPES.find(w => w.id === type)?.label || type;
}
function getWidgetIcon(type) {
  return WIDGET_TYPES.find(w => w.id === type)?.icon || '?';
}

function removeWidget(idx) {
  widgets.value.splice(idx, 1);
}

function cellOccupied(col, row, excludeIdx) {
  for (let i = 0; i < widgets.value.length; i++) {
    if (i === excludeIdx) continue;
    const w = widgets.value[i];
    if (col >= w.col && col < w.col + w.w && row >= w.row && row < w.row + w.h) return true;
  }
  return false;
}

function canPlace(col, row, w, h, excludeIdx) {
  if (col < 1 || row < 1 || col + w - 1 > gridCols.value || row + h - 1 > gridRows.value) return false;
  for (let c = col; c < col + w; c++) {
    for (let r = row; r < row + h; r++) {
      if (cellOccupied(c, r, excludeIdx)) return false;
    }
  }
  return true;
}

// Compute which grid cell (1-based) a pixel coordinate falls in
function cellFromPoint(clientX, clientY) {
  const el = gridEl.value;
  if (!el) return null;
  const rect = el.getBoundingClientRect();
  const x = clientX - rect.left;
  const y = clientY - rect.top;
  if (x < 0 || y < 0 || x > rect.width || y > rect.height) return null;
  const gap = 4; // matches gap-1 = 0.25rem ≈ 4px
  const cellW = (rect.width - gap * (gridCols.value - 1)) / gridCols.value;
  const cellH = (rect.height - gap * (gridRows.value - 1)) / gridRows.value;
  const col = Math.min(gridCols.value, Math.max(1, Math.floor(x / (cellW + gap)) + 1));
  const row = Math.min(gridRows.value, Math.max(1, Math.floor(y / (cellH + gap)) + 1));
  return { col, row };
}

// ── Drag to move ──
function onWidgetPointerDown(evt, idx) {
  if (evt.target.closest('.resize-handle') || evt.target.closest('.remove-btn')) return;
  if (evt.button !== 0) return;
  evt.preventDefault();
  addingType.value = '';
  dragging.value = true;
  dragWidgetIdx.value = idx;
  dragIsResize.value = false;
  const w = widgets.value[idx];
  dragGhostW.value = w.w;
  dragGhostH.value = w.h;
  dragGhostCol.value = w.col;
  dragGhostRow.value = w.row;
  dragGhostValid.value = true;
  document.addEventListener('pointermove', onDragPointerMove);
  document.addEventListener('pointerup', onDragPointerUp);
}

// ── Drag to resize ──
function onResizePointerDown(evt, idx) {
  if (evt.button !== 0) return;
  evt.preventDefault();
  evt.stopPropagation();
  addingType.value = '';
  dragging.value = true;
  dragWidgetIdx.value = idx;
  dragIsResize.value = true;
  const w = widgets.value[idx];
  dragGhostCol.value = w.col;
  dragGhostRow.value = w.row;
  dragGhostW.value = w.w;
  dragGhostH.value = w.h;
  dragGhostValid.value = true;
  document.addEventListener('pointermove', onDragPointerMove);
  document.addEventListener('pointerup', onDragPointerUp);
}

function onDragPointerMove(evt) {
  const cell = cellFromPoint(evt.clientX, evt.clientY);
  if (!cell) { dragGhostValid.value = false; return; }
  const idx = dragWidgetIdx.value;
  const w = widgets.value[idx];
  if (!w) return;

  if (dragIsResize.value) {
    const newW = Math.max(1, cell.col - w.col + 1);
    const newH = Math.max(1, cell.row - w.row + 1);
    dragGhostCol.value = w.col;
    dragGhostRow.value = w.row;
    dragGhostW.value = newW;
    dragGhostH.value = newH;
    dragGhostValid.value = canPlace(w.col, w.row, newW, newH, idx);
  } else {
    dragGhostCol.value = cell.col;
    dragGhostRow.value = cell.row;
    dragGhostW.value = w.w;
    dragGhostH.value = w.h;
    dragGhostValid.value = canPlace(cell.col, cell.row, w.w, w.h, idx);
  }
}

function onDragPointerUp(evt) {
  document.removeEventListener('pointermove', onDragPointerMove);
  document.removeEventListener('pointerup', onDragPointerUp);

  if (dragGhostValid.value) {
    const idx = dragWidgetIdx.value;
    const w = widgets.value[idx];
    if (w) {
      if (dragIsResize.value) {
        w.w = dragGhostW.value;
        w.h = dragGhostH.value;
      } else {
        w.col = dragGhostCol.value;
        w.row = dragGhostRow.value;
      }
    }
  }

  dragging.value = false;
  dragWidgetIdx.value = -1;
  dragGhostCol.value = -1;
  dragGhostRow.value = -1;
  dragGhostValid.value = false;
}

// ── Add from palette (click-then-click) ──
function selectPaletteWidget(typeId) {
  addingType.value = addingType.value === typeId ? '' : typeId;
  dragging.value = false;
}

function onCellClickForAdd(col, row) {
  if (!addingType.value) return;
  if (canPlace(col, row, 1, 1, -1)) {
    widgets.value.push({ type: addingType.value, col, row, w: 1, h: 1 });
  }
  addingType.value = '';
}

function cancelAdding() {
  addingType.value = '';
}

// Check if a cell is empty (not covered by any widget)
function isCellEmpty(col, row) {
  return !cellOccupied(col, row, -1);
}

function saveLayout() {
  if (!clientId.value) return;
  saving.value = true;
  writeError.value = '';
  writeOk.value = '';
  send({
    type: 'kiosk_save_layout',
    clientId: clientId.value,
    layout: currentPreset.value,
    cols: gridCols.value,
    rows: gridRows.value,
    widgets: widgets.value.map(w => ({ type: w.type, col: w.col, row: w.row, w: w.w, h: w.h, config: w.config })),
    units: units.value,
  });
  // Cleared by the kiosk_layout_saved ack (or an error event), not a timer.
  // The timeout only reports that no answer arrived at all.
  armWriteTimeout(saving, 'Save');
}

function applyLayout() {
  if (!clientId.value) return;
  applying.value = true;
  writeError.value = '';
  writeOk.value = '';
  send({
    type: 'kiosk_set',
    clientId: clientId.value,
    content: { kind: 'page', layout: currentPreset.value, units: units.value },
  });
  armWriteTimeout(applying, 'Apply');
}

// A write that draws neither an ack nor an error is itself a failure state.
function armWriteTimeout(flag, label) {
  const timer = setTimeout(() => {
    if (!flag.value) return;
    flag.value = false;
    writeError.value = `${label} timed out: no response from the server.`;
  }, 8000);
  writeTimers.push(timer);
}

function resetToDefault() {
  loadDefaultLayout(currentPreset.value);
}

// Legacy kiosk actions
function kioskSetDashboard() { if (!clientId.value) return; send({ type: 'kiosk_set', clientId: clientId.value, content: { kind: 'dashboard' } }); }
function kioskSetBlank() { if (!clientId.value) return; send({ type: 'kiosk_set', clientId: clientId.value, content: { kind: 'blank' } }); }
function kioskSetMessage() {
  if (!clientId.value || !kioskMessageText.value.trim()) return;
  send({ type: 'kiosk_set', clientId: clientId.value, content: { kind: 'message', title: kioskMessageTitle.value.trim(), text: kioskMessageText.value.trim() } });
  showKioskMessageModal.value = false; kioskMessageTitle.value = ''; kioskMessageText.value = '';
}
function kioskSetUrl() {
  if (!clientId.value || !isValidKioskUrl.value) return;
  send({ type: 'kiosk_set', clientId: clientId.value, content: { kind: 'url', url: kioskUrl.value.trim() } });
  showKioskUrlModal.value = false; kioskUrl.value = '';
}

function switchToKiosk() { if (!clientId.value || switchingVariant.value) return; switchingVariant.value = true; send({ type: 'switch_variant', clientId: clientId.value, variant: 'kiosk' }); }
function switchToHeadless() { if (!clientId.value || switchingVariant.value) return; switchingVariant.value = true; send({ type: 'switch_variant', clientId: clientId.value, variant: 'headless' }); }

// ESC key cancels any drag or palette selection
function onKeyDown(evt) {
  if (evt.key === 'Escape') {
    addingType.value = '';
    if (dragging.value) {
      document.removeEventListener('pointermove', onDragPointerMove);
      document.removeEventListener('pointerup', onDragPointerUp);
      dragging.value = false;
      dragWidgetIdx.value = -1;
      dragGhostCol.value = -1;
      dragGhostRow.value = -1;
    }
  }
}

const off = [];
onMounted(() => {
  if (clientId.value) {
    send({ type: 'kiosk_get_layouts', clientId: clientId.value });
  }
  off.push(onWS('kiosk_layouts', (msg) => {
    if (msg.clientId === clientId.value) {
      loadLayoutFromStore(currentPreset.value);
    }
  }));
  off.push(onWS('kiosk_layout_saved', (msg) => {
    if (msg.clientId && msg.clientId !== clientId.value) return;
    saving.value = false;
    if (msg.ok) {
      writeOk.value = `Layout "${msg.layout}" saved on the agent.`;
      layoutSource.value = 'agent';
      // Re-read so the editor reflects what the agent actually stored.
      send({ type: 'kiosk_get_layouts', clientId: clientId.value });
    } else {
      writeError.value = `Agent refused the save: ${msg.error || 'unknown error'}`;
    }
  }));
  off.push(onWS('kiosk_set_dispatched', (msg) => {
    if (msg.clientId !== clientId.value) return;
    applying.value = false;
    writeOk.value = 'Layout dispatched to the agent.';
  }));
  off.push(onWS('kiosk_error', (msg) => {
    saving.value = false;
    applying.value = false;
    writeError.value = msg.error || 'Kiosk command failed.';
  }));
  // Relay-level denial (e.g. "forbidden: admin role required"). Previously
  // emitted and dropped on the floor, which is how a permissions failure
  // looked exactly like a successful save.
  off.push(onWS('error', (msg) => {
    if (!String(msg.event || '').startsWith('kiosk_')) return;
    saving.value = false;
    applying.value = false;
    writeError.value = `${msg.event}: ${msg.error || 'refused by the server'}`;
  }));
  off.push(onWS('variant_switch_result', (msg) => {
    if (msg.clientId === clientId.value) switchingVariant.value = false;
  }));
  document.addEventListener('keydown', onKeyDown);
});
onUnmounted(() => {
  writeTimers.forEach(t => clearTimeout(t));
  off.forEach(fn => fn());
  document.removeEventListener('keydown', onKeyDown);
  document.removeEventListener('pointermove', onDragPointerMove);
  document.removeEventListener('pointerup', onDragPointerUp);
});

watch(clientId, (cid) => {
  if (cid) send({ type: 'kiosk_get_layouts', clientId: cid });
});

const gridCellList = computed(() => {
  const cells = [];
  for (let r = 1; r <= gridRows.value; r++) {
    for (let c = 1; c <= gridCols.value; c++) {
      cells.push({ col: c, row: r });
    }
  }
  return cells;
});

const statusHint = computed(() => {
  if (dragging.value) {
    if (dragIsResize.value) return 'Drag to resize, release to confirm';
    return 'Drag to a new position, release to drop';
  }
  if (addingType.value) {
    const label = getWidgetLabel(addingType.value);
    return `Click an empty cell to place "${label}" — press Esc to cancel`;
  }
  return 'Drag widgets to move them. Drag the corner handle to resize.';
});
</script>

<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
    <!-- Status banner -->
    <div class="flex items-center gap-3 text-sm">
      <span
        class="w-2.5 h-2.5 rounded-full flex-shrink-0"
        :class="kioskStatus?.connected ? 'bg-green-500' : (kioskStatus?.running ? 'bg-yellow-500' : 'bg-gray-400')"
      ></span>
      <span class="text-gray-700 dark:text-gray-300">
        Kiosk: <strong>{{ kioskStatus?.connected ? 'Connected' : (kioskStatus?.running ? 'Running' : 'Offline') }}</strong>
      </span>
      <span class="text-gray-500 dark:text-gray-400" v-if="kioskStatus">
        &middot; Showing: {{ kioskContentLabel }}
      </span>
      <span class="text-red-500 text-xs" v-if="kioskStatus?.lastError">
        &middot; {{ kioskStatus.lastError }}
      </span>
    </div>

    <!-- Layout preset selector -->
    <div>
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white mb-3">Page Layouts</h2>
      <div class="grid grid-cols-3 gap-3">
        <button
          v-for="preset in PRESETS" :key="preset.name"
          class="p-3 rounded-lg border-2 transition-colors text-left"
          :class="currentPreset === preset.name
            ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/30'
            : 'border-gray-200 dark:border-gray-700 hover:border-gray-400 dark:hover:border-gray-500'"
          @click="selectPreset(preset)"
        >
          <div class="font-medium text-sm text-gray-900 dark:text-white">{{ preset.label }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ preset.desc }} &middot; {{ preset.cols }}&times;{{ preset.rows }}</div>
          <div class="mt-2 grid gap-0.5" :style="{ gridTemplateColumns: `repeat(${preset.cols}, 1fr)`, gridTemplateRows: `repeat(${preset.rows}, 1fr)` }">
            <div v-for="i in preset.cols * preset.rows" :key="i" class="h-2 rounded-sm bg-gray-300 dark:bg-gray-600"></div>
          </div>
        </button>
      </div>
    </div>

    <!-- Grid editor -->
    <div class="flex gap-4">
      <div class="flex-1">
        <div class="flex items-center justify-between mb-3">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-white">Grid Editor</h2>
          <div class="flex gap-2 items-center">
            <div class="flex rounded border border-gray-300 dark:border-gray-600 overflow-hidden">
              <button @click="units = 'imperial'"
                class="px-2 py-1 text-xs transition-colors"
                :class="units === 'imperial'
                  ? 'bg-indigo-600 text-white'
                  : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'"
              >&deg;F</button>
              <button @click="units = 'metric'"
                class="px-2 py-1 text-xs transition-colors"
                :class="units === 'metric'
                  ? 'bg-indigo-600 text-white'
                  : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600'"
              >&deg;C</button>
            </div>
            <button @click="showPalette = !showPalette"
              class="px-2.5 py-1 text-xs rounded bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-200 hover:bg-gray-300 dark:hover:bg-gray-600">
              {{ showPalette ? 'Hide' : 'Add' }} Widgets
            </button>
            <button @click="resetToDefault"
              class="px-2.5 py-1 text-xs rounded bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-200 hover:bg-gray-300 dark:hover:bg-gray-600">
              Reset
            </button>
            <button @click="saveLayout" :disabled="saving || !isAdmin"
              :title="isAdmin ? '' : 'Requires the admin role'"
              class="px-2.5 py-1 text-xs rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed">
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
            <button @click="applyLayout" :disabled="applying || !isAdmin"
              :title="isAdmin ? '' : 'Requires the admin role'"
              class="px-2.5 py-1 text-xs rounded bg-green-600 text-white hover:bg-green-700 disabled:opacity-50 disabled:cursor-not-allowed">
              {{ applying ? 'Applying...' : 'Apply' }}
            </button>
          </div>
        </div>

        <!-- Write-permission / provenance / result banners -->
        <p v-if="!isAdmin"
          class="text-xs mb-2 px-2 py-1.5 rounded bg-amber-50 dark:bg-amber-900/30 text-amber-800 dark:text-amber-200 border border-amber-300 dark:border-amber-700">
          You are signed in without the admin role. Saving and applying layouts are
          admin-gated on the server, so those controls are disabled here.
        </p>
        <p v-else-if="layoutSource === 'pending'"
          class="text-xs mb-2 px-2 py-1.5 rounded bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-300 border border-gray-300 dark:border-gray-600">
          Waiting for saved layouts from the agent&hellip; showing a built-in default.
        </p>
        <p v-else-if="layoutSource === 'default'"
          class="text-xs mb-2 px-2 py-1.5 rounded bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-300 border border-gray-300 dark:border-gray-600">
          The agent has no saved &ldquo;{{ currentPreset }}&rdquo; layout. This is a built-in
          default and is not yet stored on the agent.
        </p>
        <p v-if="writeError"
          class="text-xs mb-2 px-2 py-1.5 rounded bg-red-50 dark:bg-red-900/30 text-red-700 dark:text-red-300 border border-red-300 dark:border-red-700">
          {{ writeError }}
        </p>
        <p v-else-if="writeOk"
          class="text-xs mb-2 px-2 py-1.5 rounded bg-green-50 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-300 dark:border-green-700">
          {{ writeOk }}
        </p>

        <!-- Interaction hint -->
        <p class="text-xs mb-2 h-4"
          :class="(dragging || addingType) ? 'text-indigo-600 dark:text-indigo-400 font-medium' : 'text-gray-400 dark:text-gray-500'">
          {{ statusHint }}
        </p>

        <!-- The grid -->
        <div class="relative bg-gray-100 dark:bg-gray-800 rounded-lg border border-gray-300 dark:border-gray-600 p-2">
          <div
            ref="gridEl"
            class="grid gap-1"
            :style="{ gridTemplateColumns: `repeat(${gridCols}, 1fr)`, gridTemplateRows: `repeat(${gridRows}, minmax(80px, 1fr))` }"
          >
            <!-- Background cells (always visible) -->
            <div
              v-for="cell in gridCellList" :key="`bg-${cell.col}-${cell.row}`"
              class="rounded border border-dashed transition-all duration-150"
              :class="[
                addingType && isCellEmpty(cell.col, cell.row)
                  ? 'border-indigo-400 bg-indigo-50/60 dark:bg-indigo-900/30 cursor-pointer hover:bg-indigo-100 dark:hover:bg-indigo-900/50'
                  : 'border-gray-300 dark:border-gray-600',
              ]"
              :style="{ gridColumn: cell.col, gridRow: cell.row }"
              @click="onCellClickForAdd(cell.col, cell.row)"
            ></div>

            <!-- Widget cards -->
            <div
              v-for="(widget, idx) in widgets" :key="`w-${idx}`"
              class="rounded-lg border-2 p-2 flex flex-col shadow-sm relative select-none transition-all duration-150"
              :class="[
                dragging && dragWidgetIdx === idx
                  ? 'opacity-40 border-indigo-400 bg-indigo-50 dark:bg-indigo-900/20'
                  : 'bg-white dark:bg-gray-700 border-gray-300 dark:border-gray-500 cursor-grab hover:border-indigo-400 dark:hover:border-indigo-400',
                'z-10',
              ]"
              :style="{ gridColumn: `${widget.col} / span ${widget.w}`, gridRow: `${widget.row} / span ${widget.h}` }"
              @pointerdown="onWidgetPointerDown($event, idx)"
            >
              <div class="flex items-center justify-between mb-1">
                <span class="text-xs font-medium text-gray-700 dark:text-gray-200 truncate">
                  {{ getWidgetIcon(widget.type) }} {{ getWidgetLabel(widget.type) }}
                </span>
                <button
                  class="remove-btn text-gray-400 hover:text-red-500 text-sm leading-none p-0.5 rounded hover:bg-red-50 dark:hover:bg-red-900/30"
                  @click.stop="removeWidget(idx)"
                  title="Remove widget"
                >&#10005;</button>
              </div>
              <div class="flex-1 flex items-center justify-center text-xs text-gray-400 dark:text-gray-500">
                {{ widget.col }},{{ widget.row }} &middot; {{ widget.w }}&times;{{ widget.h }}
              </div>
              <!-- Resize handle -->
              <div
                class="resize-handle absolute bottom-0.5 right-0.5 w-5 h-5 flex items-center justify-center cursor-se-resize rounded hover:bg-gray-200 dark:hover:bg-gray-600"
                @pointerdown="onResizePointerDown($event, idx)"
                title="Drag to resize"
              >
                <svg viewBox="0 0 16 16" class="w-3 h-3 text-gray-400"><path d="M14 14H10M14 14V10M14 8V6M8 14H6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
              </div>
            </div>

            <!-- Drag ghost preview -->
            <div
              v-if="dragging && dragGhostCol > 0 && dragGhostRow > 0"
              class="rounded-lg border-2 border-dashed pointer-events-none z-20 transition-all duration-75"
              :class="dragGhostValid ? 'border-indigo-500 bg-indigo-100/50 dark:bg-indigo-800/30' : 'border-red-400 bg-red-100/50 dark:bg-red-900/30'"
              :style="{
                gridColumn: `${dragGhostCol} / span ${dragGhostW}`,
                gridRow: `${dragGhostRow} / span ${dragGhostH}`,
              }"
            >
              <div class="flex items-center justify-center h-full text-xs font-medium"
                :class="dragGhostValid ? 'text-indigo-600 dark:text-indigo-300' : 'text-red-500 dark:text-red-400'">
                {{ dragGhostValid ? (dragIsResize ? `${dragGhostW}×${dragGhostH}` : 'Drop here') : 'Occupied' }}
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Widget palette sidebar -->
      <div v-if="showPalette" class="w-48 flex-shrink-0">
        <h3 class="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2 uppercase tracking-wider">Widgets</h3>
        <div class="space-y-1">
          <button
            v-for="wt in WIDGET_TYPES" :key="wt.id"
            class="w-full text-left px-2 py-1.5 rounded text-xs transition-colors"
            :class="addingType === wt.id
              ? 'bg-indigo-100 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300 ring-1 ring-indigo-400'
              : 'text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-700'"
            @click="selectPaletteWidget(wt.id)"
          >
            {{ wt.icon }} {{ wt.label }}
          </button>
        </div>
        <p class="text-xs text-gray-400 dark:text-gray-500 mt-3">
          Click a widget type, then click an empty cell to place it.
        </p>
        <button v-if="addingType" @click="cancelAdding"
          class="mt-2 w-full px-2 py-1 text-xs rounded bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-300 dark:hover:bg-gray-600">
          Cancel
        </button>
      </div>
    </div>

    <!-- Legacy / other modes -->
    <div>
      <h2 class="text-sm font-semibold text-gray-900 dark:text-white mb-3">Other Modes</h2>
      <div class="flex gap-2 flex-wrap">
        <button @click="kioskSetDashboard"
          class="px-3 py-1.5 text-xs rounded border transition-colors"
          :class="kioskStatus?.content?.kind === 'dashboard' ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300' : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'"
        >System Dashboard</button>
        <button @click="kioskSetBlank"
          class="px-3 py-1.5 text-xs rounded border transition-colors"
          :class="kioskStatus?.content?.kind === 'blank' ? 'border-gray-500 bg-gray-200 dark:bg-gray-600' : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'"
        >Blank Screen</button>
        <button @click="showKioskMessageModal = true"
          class="px-3 py-1.5 text-xs rounded border transition-colors"
          :class="kioskStatus?.content?.kind === 'message' ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/40' : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'"
        >Show Message</button>
        <button @click="showKioskUrlModal = true"
          class="px-3 py-1.5 text-xs rounded border transition-colors"
          :class="kioskStatus?.content?.kind === 'url' ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/40' : 'border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'"
        >Show URL</button>
      </div>
    </div>

    <!-- Variant controls -->
    <div class="flex items-center gap-3 text-sm border-t border-gray-200 dark:border-gray-700 pt-4">
      <span class="text-gray-500 dark:text-gray-400">Kiosk Mode:</span>
      <span
        v-if="variantStatus"
        class="font-mono px-1.5 py-0.5 rounded text-xs"
        :class="variantStatus.current === 'kiosk' ? 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200' : 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200'"
      >{{ variantStatus.current }}</span>
      <span
        v-if="variantStatus"
        class="w-2 h-2 rounded-full"
        :class="variantStatus.kioskAvailable ? 'bg-green-500' : 'bg-gray-400'"
        :title="variantStatus.kioskAvailable ? 'Kiosk support available' : 'Kiosk not available (headless build)'"
      ></span>
      <button v-if="!isKioskActive" @click="switchToKiosk" :disabled="switchingVariant"
        class="px-2 py-1 text-xs rounded bg-purple-600 text-white hover:bg-purple-700 disabled:opacity-40">
        {{ switchingVariant ? 'Switching...' : 'Enable Kiosk' }}
      </button>
      <button v-if="isKioskActive" @click="switchToHeadless" :disabled="switchingVariant"
        class="px-2 py-1 text-xs rounded bg-gray-600 text-white hover:bg-gray-700 disabled:opacity-40">
        {{ switchingVariant ? 'Switching...' : 'Disable Kiosk' }}
      </button>
      <span v-if="variantStatus?.lastSwitchError" class="text-xs text-red-500 dark:text-red-400" :title="variantStatus.lastSwitchError">Switch failed</span>
    </div>
  </div>

  <!-- Message Modal -->
  <Teleport to="body">
    <div v-if="showKioskMessageModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" @click.self="showKioskMessageModal = false">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 w-full max-w-md">
        <h3 class="text-lg font-semibold mb-4 text-gray-900 dark:text-white">Display Message</h3>
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Title (optional)</label>
            <input v-model="kioskMessageTitle" type="text" class="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white" placeholder="Welcome" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Message</label>
            <textarea v-model="kioskMessageText" rows="4" class="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white" placeholder="Enter the message to display..."></textarea>
          </div>
        </div>
        <div class="flex justify-end space-x-3 mt-6">
          <button class="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200" @click="showKioskMessageModal = false">Cancel</button>
          <button class="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50" :disabled="!kioskMessageText.trim()" @click="kioskSetMessage">Display</button>
        </div>
      </div>
    </div>
  </Teleport>

  <!-- URL Modal -->
  <Teleport to="body">
    <div v-if="showKioskUrlModal" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" @click.self="showKioskUrlModal = false">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 w-full max-w-md">
        <h3 class="text-lg font-semibold mb-4 text-gray-900 dark:text-white">Display URL</h3>
        <div>
          <label class="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">URL</label>
          <input v-model="kioskUrl" type="url" class="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-white" placeholder="https://example.com" />
          <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">Must be http:// or https://</p>
        </div>
        <div class="flex justify-end space-x-3 mt-6">
          <button class="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200" @click="showKioskUrlModal = false">Cancel</button>
          <button class="px-4 py-2 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50" :disabled="!isValidKioskUrl" @click="kioskSetUrl">Display</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
