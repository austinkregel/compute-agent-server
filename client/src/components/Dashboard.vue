<template>
  <div>
    <div v-if="activeClient && hasStats">
      <!-- Host Identity Strip -->
      <div class="rounded-lg bg-white dark:bg-gray-800 p-4 mb-4 flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-3 min-w-0">
          <div class="min-w-0">
            <div class="text-base font-semibold text-gray-900 dark:text-gray-100 truncate">{{ currentStats.hostname || activeClient.clientId }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400 truncate">
              {{ currentStats.platform }} {{ currentStats.release }} / {{ currentStats.arch }}
              <span v-if="currentStats.cpus" class="ml-1">&mdash; {{ currentStats.cpus }} cores</span>
            </div>
          </div>
        </div>
        <div class="flex items-center gap-4 text-xs text-gray-500 dark:text-gray-400 flex-shrink-0">
          <span class="font-mono">Agent {{ currentStats.agentVersion || currentStats?.agent?.version || '-' }}</span>
          <span class="font-semibold" :class="uptimeColor">Up {{ formatUptime(currentStats.uptimeSec) }}</span>
          <span v-if="lastUpdatedLocal" class="font-mono text-gray-400 dark:text-gray-500">{{ lastUpdatedLocal }}</span>
          <span v-if="!connected" class="text-red-500 dark:text-red-400 font-medium">Disconnected</span>
        </div>
      </div>

      <!-- Hero KPI Cards -->
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-4">
        <!-- CPU Load -->
        <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">CPU Load</div>
          <div class="text-2xl font-bold mb-1" :class="cpuHealthColor">{{ cpuPrimary }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mb-3 truncate">{{ cpuSubtitle }}</div>
          <CpuSparkline :data="cpuSeries" :width="200" :height="32" :label="`CPU load sparkline for ${activeClient.clientId}`" />
        </div>

        <!-- Memory -->
        <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Memory</div>
          <div class="text-2xl font-bold mb-1" :class="memHealthColor">{{ memPrimary }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mb-3 truncate">{{ memSubtitle }}</div>
          <MemorySparkline :data="memPctSeries" :width="200" :height="32" :min="0" :max="100" :label="`Memory percent trend for ${activeClient.clientId}`" />
        </div>

        <!-- Disk (worst mount) -->
        <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Disk Usage</div>
          <div class="text-2xl font-bold mb-1" :class="diskHealthColor">{{ diskPrimary }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400 font-mono mb-3 truncate">{{ diskSubtitle }}</div>
          <div class="h-2 w-full rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden">
            <div class="h-2 rounded-full transition-all duration-300" :class="diskBarColor" :style="{ width: diskPctValue + '%' }"></div>
          </div>
        </div>

        <!-- 4th card: Battery or Updates -->
        <div v-if="hasBattery" class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Battery</div>
          <div class="text-2xl font-bold mb-1" :class="batteryHealthColor">{{ batteryPrimary }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400 mb-3 truncate">{{ batterySubtitle }}</div>
          <BatterySparkline :data="batterySeries" :width="200" :height="32" :min="0" :max="100" :label="`Battery level trend for ${activeClient.clientId}`" />
        </div>

        <div v-else class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">OS Updates</div>
          <div class="text-2xl font-bold mb-1" :class="updatesHealthColor">{{ updatesPrimary }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400 mb-3 truncate">{{ updatesSubtitle }}</div>
          <Sparkline :data="backupSeries" :width="200" :height="32" class="text-green-500 dark:text-green-400" :label="`Backups count trend for ${activeClient.clientId}`" />
        </div>
      </div>

      <SystemDetails :client-id="activeClient.clientId" :stats="currentStats" :mem-series="memPctSeries" />
    </div>

    <div v-else class="py-16 text-center">
      <div class="text-gray-400 dark:text-gray-500 mb-2">
        <svg class="w-12 h-12 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" /></svg>
      </div>
      <div class="text-sm text-gray-500 dark:text-gray-400">
        <span v-if="!activeClient">No client selected.</span>
        <span v-else>Waiting for stats from {{ activeClient.clientId }}...</span>
      </div>
    </div>
  </div>
</template>
<script setup>
import { computed } from 'vue';
import { connected, clientIds, statsMap, statsHistory, clientHasCapability, on as onWS } from '../lib/sharedWS.js';
import Sparkline from './Sparkline.vue';
import CpuSparkline from './CpuSparkline.vue';
import MemorySparkline from './MemorySparkline.vue';
import BatterySparkline from './BatterySparkline.vue';
import SystemDetails from './SystemDetails.vue';

const props = defineProps({ selectedClient: { type: String, default: '' } });

const activeClient = computed(() => {
  if (props.selectedClient && clientIds.value.includes(props.selectedClient)) return props.selectedClient;
  return clientIds.value.find(client => client.clientId === props.selectedClient);
});
const hasStats = computed(() => !!(activeClient.value && statsMap[activeClient.value.clientId]));
const currentStats = computed(() => statsMap[activeClient.value?.clientId] || {});

const history = computed(() => statsHistory[activeClient.value?.clientId] || []);
const maxPoints = 15;
const tail = (arr) => arr.slice(-maxPoints);
const cpuSeries = computed(() => tail(history.value).map(s => s.load?.['1m'] ?? 0));
const memPctSeries = computed(() => tail(history.value).map(s => {
  const m = s.mem; return m ? (m.used / m.total) * 100 : 0;
}));
const backupSeries = computed(() => tail(history.value).map(s => (s.backups ?? 0)));

const lastUpdatedLocal = computed(() => {
  if (!history.value.length) return '';
  try { return new Date(history.value[history.value.length - 1].ts).toLocaleTimeString(); } catch { return ''; }
});

// --- Uptime ---
function formatUptime(sec) {
  if (typeof sec !== 'number') return '-';
  const d = Math.floor(sec / 86400); sec %= 86400;
  const h = Math.floor(sec / 3600); sec %= 3600;
  const m = Math.floor(sec / 60);
  const parts = []; if (d) parts.push(d + 'd'); if (h) parts.push(h + 'h'); if (m) parts.push(m + 'm');
  return parts.length ? parts.join(' ') : '<1m';
}
const uptimeColor = computed(() => {
  const sec = currentStats.value?.uptimeSec;
  if (typeof sec !== 'number') return 'text-gray-500 dark:text-gray-400';
  if (sec > 86400) return 'text-green-600 dark:text-green-400';
  if (sec > 3600) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
});

// --- CPU Hero ---
const cpuLoad1m = computed(() => {
  const load = currentStats.value?.load;
  if (load && typeof load['1m'] === 'number') return load['1m'];
  const cpu = currentStats.value?.cpu;
  if (typeof cpu === 'number') return cpu;
  return null;
});
const cpuPrimary = computed(() => {
  const v = cpuLoad1m.value;
  return v !== null ? v.toFixed(2) : '-';
});
const cpuSubtitle = computed(() => {
  const load = currentStats.value?.load;
  if (!load) return '';
  const a = load['1m'], b = load['5m'], c = load['15m'];
  if ([a, b, c].some(v => typeof v !== 'number')) return '';
  return `${a.toFixed(2)} / ${b.toFixed(2)} / ${c.toFixed(2)}`;
});
const cpuHealthColor = computed(() => {
  const v = cpuLoad1m.value;
  const cores = currentStats.value?.cpus;
  if (v === null || !cores) return 'text-gray-900 dark:text-gray-100';
  if (v < cores * 0.5) return 'text-green-600 dark:text-green-400';
  if (v < cores * 0.8) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
});

// --- Memory Hero ---
const memPct = computed(() => {
  const mem = currentStats.value?.mem;
  if (!mem || !mem.total) return null;
  return (mem.used / mem.total) * 100;
});
const memPrimary = computed(() => {
  const p = memPct.value;
  return p !== null ? p.toFixed(1) + '%' : '-';
});
const memSubtitle = computed(() => {
  const mem = currentStats.value?.mem;
  if (!mem) return '';
  return humanBytes(mem.used) + ' of ' + humanBytes(mem.total);
});
const memHealthColor = computed(() => {
  const p = memPct.value;
  if (p === null) return 'text-gray-900 dark:text-gray-100';
  if (p < 60) return 'text-green-600 dark:text-green-400';
  if (p < 80) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
});

// --- Disk Hero (worst mount) ---
function parseDiskPct(d) {
  if (!d) return 0;
  const cap = d.capacity;
  if (typeof cap === 'number') return cap;
  if (typeof cap === 'string') {
    const n = parseFloat(cap);
    return isNaN(n) ? 0 : n;
  }
  return 0;
}
const worstDisk = computed(() => {
  const disks = currentStats.value?.disk;
  if (!Array.isArray(disks) || !disks.length) return null;
  let worst = disks[0];
  for (const d of disks) {
    if (parseDiskPct(d) > parseDiskPct(worst)) worst = d;
  }
  return worst;
});
const diskPctValue = computed(() => worstDisk.value ? parseDiskPct(worstDisk.value) : 0);
const diskPrimary = computed(() => {
  const p = diskPctValue.value;
  return worstDisk.value ? p.toFixed(0) + '%' : '-';
});
const diskSubtitle = computed(() => {
  const d = worstDisk.value;
  if (!d) return 'No disks';
  return d.mount || '-';
});
const diskHealthColor = computed(() => {
  const p = diskPctValue.value;
  if (!worstDisk.value) return 'text-gray-900 dark:text-gray-100';
  if (p < 70) return 'text-green-600 dark:text-green-400';
  if (p < 85) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
});
const diskBarColor = computed(() => {
  const p = diskPctValue.value;
  if (p < 70) return 'bg-green-500 dark:bg-green-400';
  if (p < 85) return 'bg-yellow-500 dark:bg-yellow-400';
  return 'bg-red-500 dark:bg-red-400';
});

// --- Battery Hero ---
const primaryBattery = computed(() => {
  const batteries = currentStats.value?.battery?.devices;
  return batteries?.[0] || null;
});
// Additive migration to the generic capability registry (see DockerOverview.vue);
// falls back to ad hoc presence-sniffing until the agent reports a "battery"
// capability (agent-side migration tracked separately).
const hasBattery = computed(() =>
  clientHasCapability(activeClient.value?.clientId, 'battery') || primaryBattery.value !== null
);
const batterySeries = computed(() =>
  tail(history.value).map(s => s.battery?.devices?.[0]?.percent ?? null).filter(v => v !== null)
);
const batteryPrimary = computed(() => {
  const b = primaryBattery.value;
  if (!b || typeof b.percent !== 'number') return '-';
  return b.percent.toFixed(0) + '%';
});
const batterySubtitle = computed(() => {
  const b = primaryBattery.value;
  if (!b) return '';
  return b.status ? b.status.charAt(0).toUpperCase() + b.status.slice(1) : '';
});
const batteryHealthColor = computed(() => {
  const b = primaryBattery.value;
  if (!b) return 'text-gray-900 dark:text-gray-100';
  if (b.status === 'charging') return 'text-blue-600 dark:text-blue-400';
  if (b.percent > 50) return 'text-green-600 dark:text-green-400';
  if (b.percent > 20) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
});

// --- Updates Hero (fallback for 4th card) ---
const updatesData = computed(() => currentStats.value?.updates || null);
const updatesPrimary = computed(() => {
  const u = updatesData.value;
  if (!u || typeof u.available !== 'number') return '-';
  return String(u.available);
});
const updatesSubtitle = computed(() => {
  const u = updatesData.value;
  if (!u) return 'No data';
  const parts = [];
  if (typeof u.available === 'number') parts.push(u.available + ' available');
  if (typeof u.security === 'number' && u.security > 0) parts.push(u.security + ' security');
  if (u.restartRequired) parts.push('restart required');
  return parts.join(', ') || 'Up to date';
});
const updatesHealthColor = computed(() => {
  const u = updatesData.value;
  if (!u || typeof u.available !== 'number') return 'text-gray-900 dark:text-gray-100';
  if (u.available === 0) return 'text-green-600 dark:text-green-400';
  if ((u.security || 0) > 0 || u.restartRequired) return 'text-red-600 dark:text-red-400';
  return 'text-yellow-600 dark:text-yellow-400';
});

function humanBytes(v) {
  if (!v || isNaN(v)) return v;
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0; let n = Number(v);
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return n.toFixed(n >= 10 ? 0 : 1) + units[i];
}

onWS('command_output', () => {});
</script>
