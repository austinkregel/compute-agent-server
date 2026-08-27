<template>
  <div v-if="stats">
    <AlertsPanel v-if="clientId" :client-id="clientId" />

    <!-- Primary info: 2-column grid on desktop -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
      <!-- System card -->
      <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
        <div class="flex items-center justify-between mb-4">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">System</h3>
          <span class="text-xs text-gray-400 dark:text-gray-500 font-mono">{{ shortTs(stats.ts) }}</span>
        </div>
        <dl class="grid grid-cols-2 gap-x-6 gap-y-3">
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">Agent</dt>
            <dd class="text-sm font-mono font-medium text-gray-900 dark:text-gray-100 truncate">{{ stats.agentVersion || stats?.agent?.version || '-' }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">Platform</dt>
            <dd class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ stats.platform }} {{ stats.release }} / {{ stats.arch }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">CPU Cores</dt>
            <dd class="text-sm font-semibold text-blue-600 dark:text-blue-400">{{ stats.cpus }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">Uptime</dt>
            <dd class="text-sm font-semibold text-green-600 dark:text-green-400">{{ formatUptime(stats.uptimeSec) }}</dd>
          </div>
          <div>
            <dt class="text-xs text-gray-500 dark:text-gray-400">Load Average</dt>
            <dd class="text-sm font-mono text-gray-900 dark:text-gray-100">{{ loadTriple(stats.load) }}</dd>
          </div>
          <div v-if="cpuTemp">
            <dt class="text-xs text-gray-500 dark:text-gray-400">CPU Temp</dt>
            <dd class="text-sm font-semibold text-orange-600 dark:text-orange-400">{{ cpuTemp }}</dd>
          </div>
          <div v-for="(gpu, idx) in gpuTemps" :key="'gpu-'+idx">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ gpu.label }} Temp</dt>
            <dd class="text-sm font-semibold text-purple-600 dark:text-purple-400">{{ gpu.value }}</dd>
          </div>
          <div v-for="(nvme, idx) in nvmeTemps" :key="'nvme-'+idx">
            <dt class="text-xs text-gray-500 dark:text-gray-400">{{ nvme.label }} Temp</dt>
            <dd class="text-sm font-semibold text-indigo-600 dark:text-indigo-400">{{ nvme.value }}</dd>
          </div>
        </dl>
      </div>

      <!-- Memory + OS Updates stacked on the right -->
      <div class="flex flex-col gap-4">
        <!-- Memory card -->
        <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-4">Memory</h3>
          <dl class="grid grid-cols-3 gap-x-6 gap-y-1 mb-3">
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Used</dt>
              <dd class="text-sm font-mono font-semibold text-gray-900 dark:text-gray-100">{{ humanBytes(stats.mem.used) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Free</dt>
              <dd class="text-sm font-mono font-semibold text-gray-900 dark:text-gray-100">{{ humanBytes(stats.mem.free) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Total</dt>
              <dd class="text-sm font-mono font-semibold text-gray-900 dark:text-gray-100">{{ humanBytes(stats.mem.total) }}</dd>
            </div>
          </dl>
          <div class="h-3 w-full rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden">
            <div
              class="h-3 rounded-full transition-all duration-300"
              :class="memBarColor"
              :style="{ width: memPercent + '%' }"
            ></div>
          </div>
          <div class="mt-1.5 text-xs font-mono text-gray-500 dark:text-gray-400 text-right">{{ memPercent.toFixed(1) }}%</div>
        </div>

        <!-- OS Updates card -->
        <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
          <div class="flex items-center justify-between mb-4">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">OS Updates</h3>
            <button
              class="text-xs px-2.5 py-1 rounded border border-gray-200 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              :disabled="!connected || !clientId"
              @click="checkUpdates"
              title="Request an immediate update check from the agent"
            >
              Check now
            </button>
          </div>
          <div v-if="updates && updates.checkError" class="text-xs text-red-600 dark:text-red-400 mb-3">
            Update check failed: <span class="font-mono">{{ updates.checkError }}</span>
          </div>
          <dl class="grid grid-cols-2 gap-x-6 gap-y-3">
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Available</dt>
              <dd class="text-sm font-mono font-semibold" :class="(updates?.available||0) > 0 ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'">
                {{ (updates && typeof updates.available === 'number') ? updates.available : '-' }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Security</dt>
              <dd class="text-sm font-mono font-semibold" :class="(updates?.security||0) > 0 ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-gray-100'">
                {{ (updates && typeof updates.security === 'number') ? updates.security : '-' }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Restart</dt>
              <dd class="text-sm font-mono font-semibold" :class="updates?.restartRequired ? 'text-orange-600 dark:text-orange-400' : 'text-gray-900 dark:text-gray-100'">
                {{ updates ? (updates.restartRequired ? 'required' : 'no') : '-' }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500 dark:text-gray-400">Checked</dt>
              <dd class="text-sm font-mono text-gray-900 dark:text-gray-100 truncate" :title="updates?.lastChecked || ''">
                {{ updates ? shortTs(updates.lastChecked) : '-' }}
              </dd>
            </div>
          </dl>
          <div v-if="updates && (updates.available || 0) > 0" class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            {{ updates.available }} update(s) pending
          </div>
        </div>
      </div>
    </div>

    <!-- Resource cards: Disks + Network -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
      <!-- Disks card (default expanded, with bars) -->
      <div
        v-if="Array.isArray(stats.disk) && stats.disk.length"
        class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden lg:col-span-2"
      >
        <button class="flex items-center justify-between w-full text-left p-5" @click="showDisks = !showDisks">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Disks</h3>
          <div class="flex items-center gap-2">
            <span class="text-xs text-gray-400 dark:text-gray-500">{{ stats.disk.length }}</span>
            <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showDisks }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </button>
        <div v-if="showDisks" class="px-5 pb-5">
          <div class="overflow-auto text-xs">
            <table class="w-full border-collapse">
              <thead>
                <tr class="text-left text-gray-500 dark:text-gray-400">
                  <th class="py-1.5 pr-4 font-medium">Mount</th>
                  <th class="py-1.5 pr-4 font-medium" style="min-width: 140px">Usage</th>
                  <th class="py-1.5 pr-4 font-medium">Used</th>
                  <th class="py-1.5 pr-4 font-medium">Avail</th>
                  <th class="py-1.5 font-medium">FS</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="d in stats.disk" :key="d.fsname + d.mount" class="border-t border-gray-100 dark:border-gray-700/50">
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100 truncate" :title="d.mount">{{ d.mount }}</td>
                  <td class="py-1.5 pr-4">
                    <div class="flex items-center gap-2">
                      <div class="w-20 h-1.5 rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden flex-shrink-0">
                        <div class="h-1.5 rounded-full transition-all duration-300" :class="diskRowBarColor(d)" :style="{ width: diskRowPct(d) + '%' }"></div>
                      </div>
                      <span class="font-mono text-gray-900 dark:text-gray-100 whitespace-nowrap">{{ diskRowPct(d).toFixed(0) }}%</span>
                    </div>
                  </td>
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100">{{ humanKB(d.used) }}</td>
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100">{{ humanKB(d.avail) }}</td>
                  <td class="py-1.5 font-mono text-gray-700 dark:text-gray-300 truncate" :title="d.fsname">{{ shortFs(d.fsname) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>

      <!-- Network card (default expanded) -->
      <div
        v-if="Array.isArray(stats.netIfaces) && stats.netIfaces.length"
        class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden lg:col-span-2"
      >
        <button class="flex items-center justify-between w-full text-left p-5" @click="showNetwork = !showNetwork">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Network</h3>
          <div class="flex items-center gap-2">
            <span class="text-xs text-gray-400 dark:text-gray-500">{{ allIfaces.length }}</span>
            <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showNetwork }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </button>
        <div v-if="showNetwork" class="px-5 pb-5">
          <div class="overflow-auto max-h-48 text-xs">
            <table class="w-full border-collapse">
              <thead>
                <tr class="text-left text-gray-500 dark:text-gray-400">
                  <th class="py-1.5 pr-4 font-medium">Name</th>
                  <th class="py-1.5 pr-4 font-medium">Family</th>
                  <th class="py-1.5 pr-4 font-medium">Address</th>
                  <th class="py-1.5 font-medium">Internal</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="n in allIfaces" :key="n.name + n.address" class="border-t border-gray-100 dark:border-gray-700/50">
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100">{{ n.name }}</td>
                  <td class="py-1.5 pr-4 text-gray-700 dark:text-gray-300">{{ n.family }}</td>
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100 truncate" :title="n.cidr">{{ n.address }}</td>
                  <td class="py-1.5 text-gray-700 dark:text-gray-300">{{ n.internal ? 'yes' : 'no' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>

    <!-- Collapsible secondary cards -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <!-- Battery card -->
      <div
        v-if="Array.isArray(batteryDevices) && batteryDevices.length"
        class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden"
      >
        <button class="flex items-center justify-between w-full text-left p-5" @click="showBattery = !showBattery">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Battery</h3>
          <div class="flex items-center gap-2">
            <span class="text-xs text-gray-400 dark:text-gray-500">{{ batteryDevices.length }} device(s)</span>
            <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showBattery }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </button>
        <div v-if="showBattery" class="px-5 pb-5 space-y-4">
          <div v-for="b in batteryDevices" :key="b.id">
            <div class="flex items-center gap-3 mb-2">
              <div class="w-2 h-2 rounded-full flex-shrink-0" :class="batteryIconBgClass(b)"></div>
              <span class="font-mono text-sm font-semibold text-gray-900 dark:text-gray-100">{{ b.id }}</span>
              <span class="text-xs capitalize" :class="batteryStatusTextClass(b)">{{ batteryStatus(b.status) }}</span>
              <span class="ml-auto text-lg font-bold" :class="batteryPercentTextClass(b)">
                {{ typeof b.percent === 'number' ? b.percent.toFixed(0) : '-' }}<span class="text-xs font-normal">%</span>
              </span>
              <span v-if="batteryEta(b) !== '-'" class="text-xs text-gray-500 dark:text-gray-400">{{ batteryEta(b) }}</span>
            </div>
            <div class="h-1.5 w-full rounded-full bg-gray-200 dark:bg-gray-700 overflow-hidden mb-3">
              <div
                class="h-1.5 rounded-full transition-all duration-500"
                :class="batteryBarClass(b)"
                :style="{ width: (b.percent || 0) + '%' }"
              ></div>
            </div>
            <dl class="grid grid-cols-2 gap-x-6 gap-y-2">
              <div v-if="typeof b.tempC === 'number'">
                <dt class="text-xs text-gray-500 dark:text-gray-400">Temp</dt>
                <dd class="text-sm font-mono font-medium" :class="batteryTempClass(b)">{{ b.tempC.toFixed(1) }}°C</dd>
              </div>
              <div v-if="typeof b.powerNowW === 'number' && b.powerNowW > 0">
                <dt class="text-xs text-gray-500 dark:text-gray-400">Power</dt>
                <dd class="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">{{ b.powerNowW.toFixed(1) }}W</dd>
              </div>
              <div>
                <dt class="text-xs text-gray-500 dark:text-gray-400">Energy</dt>
                <dd class="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">{{ formatEnergy(b) }}</dd>
              </div>
              <div v-if="typeof b.voltageNowV === 'number' && b.voltageNowV > 0">
                <dt class="text-xs text-gray-500 dark:text-gray-400">Voltage</dt>
                <dd class="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">{{ b.voltageNowV.toFixed(2) }}V</dd>
              </div>
              <div v-if="b.cycleCount > 0">
                <dt class="text-xs text-gray-500 dark:text-gray-400">Cycles</dt>
                <dd class="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">{{ b.cycleCount }}</dd>
              </div>
              <div v-if="b.cycleCount > 0">
                <dt class="text-xs text-gray-500 dark:text-gray-400">Health</dt>
                <dd class="text-sm font-medium" :class="batteryHealthClass(b)">{{ batteryHealthLabel(b) }}</dd>
              </div>
            </dl>
          </div>
        </div>
      </div>

      <!-- Thermal card -->
      <div
        v-if="Array.isArray(thermalRows) && thermalRows.length"
        class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden"
      >
        <button class="flex items-center justify-between w-full text-left p-5" @click="showThermal = !showThermal">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Thermal Sensors</h3>
          <div class="flex items-center gap-2">
            <span class="text-xs text-gray-400 dark:text-gray-500">{{ thermalRows.length }}</span>
            <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showThermal }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </div>
        </button>
        <div v-if="showThermal" class="px-5 pb-5">
          <div class="flex items-center justify-between mb-3">
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ showRawThermals ? 'All sensors' : 'Summary (max per component)' }}</span>
            <button
              class="text-xs text-blue-600 dark:text-blue-400 hover:underline transition-colors"
              @click="showRawThermals = !showRawThermals"
            >
              {{ showRawThermals ? 'Show summary' : 'Show all sensors' }}
            </button>
          </div>
          <dl v-if="!showRawThermals" class="grid grid-cols-2 gap-x-6 gap-y-3">
            <div v-for="v in thermalSummary" :key="'t-'+v.key">
              <dt class="text-xs text-gray-500 dark:text-gray-400 truncate">{{ v.label }}</dt>
              <dd class="text-sm font-mono font-medium text-gray-900 dark:text-gray-100">{{ v.value }}</dd>
            </div>
          </dl>
          <div v-else class="overflow-auto max-h-48 text-xs">
            <table class="w-full border-collapse">
              <thead>
                <tr class="text-left text-gray-500 dark:text-gray-400">
                  <th class="py-1.5 pr-4 font-medium">Component</th>
                  <th class="py-1.5 pr-4 font-medium">Sensor</th>
                  <th class="py-1.5 pr-4 font-medium">Temp</th>
                  <th class="py-1.5 pr-4 font-medium">High</th>
                  <th class="py-1.5 font-medium">Critical</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="s in thermalRows" :key="s.sensorKey" class="border-t border-gray-100 dark:border-gray-700/50">
                  <td class="py-1.5 pr-4 text-gray-700 dark:text-gray-300">{{ s.component || '-' }}</td>
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100 truncate" :title="s.sensorKey">{{ s.name || s.sensorKey }}</td>
                  <td class="py-1.5 pr-4 font-mono text-gray-900 dark:text-gray-100">{{ typeof s.temperature === 'number' ? s.temperature.toFixed(1)+'°C' : '-' }}</td>
                  <td class="py-1.5 pr-4 font-mono text-gray-500 dark:text-gray-400">{{ typeof s.sensorHigh === 'number' && s.sensorHigh ? s.sensorHigh.toFixed(1)+'°C' : '-' }}</td>
                  <td class="py-1.5 font-mono text-gray-500 dark:text-gray-400">{{ typeof s.sensorCritical === 'number' && s.sensorCritical ? s.sensorCritical.toFixed(1)+'°C' : '-' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue';
import { connected, send } from '../lib/sharedWS.js';
import AlertsPanel from './AlertsPanel.vue';

const props = defineProps({
  clientId: { type: String, required: false, default: '' },
  stats: { type: Object, required: false },
  memSeries: { type: Array, required: false, default: () => [] }
});

const showBattery = ref(true);
const showNetwork = ref(true);
const showThermal = ref(false);
const showDisks = ref(true);
const showRawThermals = ref(false);

function shortTs(ts) { if (!ts) return ''; try { return new Date(ts).toLocaleTimeString(); } catch { return ts; } }
function loadTriple(load) {
  if (!load) return '-';
  const a = load['1m'], b = load['5m'], c = load['15m'];
  if ([a, b, c].some(v => typeof v !== 'number')) return '-';
  return `${a.toFixed(2)} / ${b.toFixed(2)} / ${c.toFixed(2)}`;
}
function humanBytes(v) {
  if (!v && v !== 0) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let n = Number(v); let i = 0; while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return (n >= 10 ? n.toFixed(0) : n.toFixed(1)) + units[i];
}
function humanKB(k) {
  if (k == null) return '-';
  return humanBytes(k);
}
function shortFs(fs) {
  if (!fs) return '';
  if (fs.startsWith('/dev/')) return fs.replace('/dev/', '');
  return fs.length > 22 ? fs.slice(0, 10) + '…' + fs.slice(-8) : fs;
}
function formatUptime(sec) {
  if (typeof sec !== 'number') return '-';
  const d = Math.floor(sec / 86400); sec %= 86400;
  const h = Math.floor(sec / 3600); sec %= 3600;
  const m = Math.floor(sec / 60);
  const parts = []; if (d) parts.push(d + 'd'); if (h) parts.push(h + 'h'); if (m) parts.push(m + 'm');
  return parts.length ? parts.join(' ') : '<1m';
}

// --- Disk helpers ---
function diskRowPct(d) {
  if (!d) return 0;
  const cap = d.capacity;
  if (typeof cap === 'number') return cap;
  if (typeof cap === 'string') { const n = parseFloat(cap); return isNaN(n) ? 0 : n; }
  return 0;
}
function diskRowBarColor(d) {
  const p = diskRowPct(d);
  if (p < 70) return 'bg-green-500 dark:bg-green-400';
  if (p < 85) return 'bg-yellow-500 dark:bg-yellow-400';
  return 'bg-red-500 dark:bg-red-400';
}

// --- Memory bar color ---
const memPercent = computed(() => props.stats?.mem ? (props.stats.mem.used / props.stats.mem.total) * 100 : 0);
const memBarColor = computed(() => {
  const p = memPercent.value;
  if (p < 60) return 'bg-green-500 dark:bg-green-400';
  if (p < 80) return 'bg-yellow-500 dark:bg-yellow-400';
  return 'bg-red-500 dark:bg-red-400';
});

// --- Battery helpers ---
function batteryStatus(s) {
  if (!s) return '-';
  const v = String(s).toLowerCase();
  if (v === 'charging') return 'charging';
  if (v === 'discharging') return 'discharging';
  if (v === 'full') return 'full';
  return v;
}
function formatEta(sec) {
  if (typeof sec !== 'number' || !isFinite(sec) || sec <= 0) return '-';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (h <= 0) return `${m}m`;
  return `${h}h ${m}m`;
}
function batteryEta(b) {
  if (!b || typeof b !== 'object') return '-';
  if (typeof b.timeToEmptySec === 'number' && b.timeToEmptySec > 0) return `-${formatEta(b.timeToEmptySec)}`;
  if (typeof b.timeToFullSec === 'number' && b.timeToFullSec > 0) return `+${formatEta(b.timeToFullSec)}`;
  return '-';
}
function batteryIconBgClass(b) {
  if (!b) return 'bg-gray-500';
  if (b.status === 'charging') return 'bg-blue-500';
  if (b.status === 'full') return 'bg-green-500';
  if (b.percent > 50) return 'bg-green-500';
  if (b.percent > 20) return 'bg-yellow-500';
  return 'bg-red-500';
}
function batteryStatusTextClass(b) {
  if (!b) return 'text-gray-500 dark:text-gray-400';
  if (b.status === 'charging') return 'text-blue-600 dark:text-blue-400';
  if (b.status === 'full') return 'text-green-600 dark:text-green-400';
  if (b.status === 'discharging') return 'text-orange-600 dark:text-orange-400';
  return 'text-gray-500 dark:text-gray-400';
}
function batteryPercentTextClass(b) {
  if (!b) return 'text-gray-900 dark:text-gray-100';
  if (b.status === 'charging') return 'text-blue-600 dark:text-blue-400';
  if (b.percent > 50) return 'text-green-600 dark:text-green-400';
  if (b.percent > 20) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
}
function batteryBarClass(b) {
  if (!b) return 'bg-gray-400';
  if (b.status === 'charging') return 'bg-blue-500 dark:bg-blue-400';
  if (b.percent > 50) return 'bg-green-500 dark:bg-green-400';
  if (b.percent > 20) return 'bg-yellow-500 dark:bg-yellow-400';
  return 'bg-red-500 dark:bg-red-400';
}
function batteryTempClass(b) {
  if (!b || typeof b.tempC !== 'number') return 'text-gray-500 dark:text-gray-400';
  if (b.tempC > 45) return 'text-red-600 dark:text-red-400';
  if (b.tempC > 35) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-green-600 dark:text-green-400';
}
function formatEnergy(b) {
  if (!b) return '-';
  const now = b.energyNowWh;
  const full = b.energyFullWh;
  if (typeof now === 'number' && now > 0) {
    if (typeof full === 'number' && full > 0) return `${now.toFixed(1)}/${full.toFixed(1)}Wh`;
    return `${now.toFixed(1)}Wh`;
  }
  return '-';
}
function batteryHealthLabel(b) {
  if (!b || typeof b.cycleCount !== 'number' || b.cycleCount <= 0) return '-';
  if (b.cycleCount < 300) return 'Excellent';
  if (b.cycleCount < 500) return 'Good';
  if (b.cycleCount < 800) return 'Fair';
  return 'Replace Soon';
}
function batteryHealthClass(b) {
  if (!b || typeof b.cycleCount !== 'number' || b.cycleCount <= 0) return 'text-gray-500';
  if (b.cycleCount < 300) return 'text-green-600 dark:text-green-400';
  if (b.cycleCount < 500) return 'text-green-600 dark:text-green-400';
  if (b.cycleCount < 800) return 'text-yellow-600 dark:text-yellow-400';
  return 'text-red-600 dark:text-red-400';
}

const clientId = computed(() => props.clientId || '');
const updates = computed(() => props.stats?.updates || null);

function checkUpdates() {
  const cid = clientId.value;
  if (!cid) return;
  send({ type: 'check_updates_request', clientId: cid });
}

const allIfaces = computed(() => (props.stats?.netIfaces || []).slice().sort((a, b) => {
  if (a.internal !== b.internal) return a.internal ? 1 : -1;
  return a.address.localeCompare(b.address);
}));
const batteryDevices = computed(() => props.stats?.battery?.devices || []);
const thermalRows = computed(() => {
  const arr = Array.isArray(props.stats?.thermal) ? props.stats.thermal : [];
  const mapped = arr.map((s) => ({
    sensorKey: s?.sensorKey || '',
    component: s?.component || guessComponentFromKey(s?.sensorKey),
    name: s?.name || guessNameFromKey(s?.sensorKey),
    temperature: s?.temperature,
    sensorHigh: s?.sensorHigh,
    sensorCritical: s?.sensorCritical,
  })).filter(r => r.sensorKey);
  mapped.sort((a, b) => (a.component || '').localeCompare(b.component || '') || a.sensorKey.localeCompare(b.sensorKey));
  return mapped;
});

const cpuTemp = computed(() => {
  const rows = thermalRows.value || [];
  const cpuRows = rows.filter(r => (r.component || '') === 'CPU');
  const temp = maxTemp(cpuRows);
  return isNum(temp) ? temp.toFixed(1) + '°C' : null;
});

const gpuTemps = computed(() => {
  const rows = thermalRows.value || [];
  const gpuRows = rows.filter(r => (r.component || '') === 'GPU');
  if (!gpuRows.length) return [];
  const groups = new Map();
  for (const r of gpuRows) {
    const gk = groupKeyFromSensorKey(r.sensorKey) || 'gpu';
    if (!groups.has(gk)) groups.set(gk, []);
    groups.get(gk).push(r);
  }
  const keys = Array.from(groups.keys()).sort();
  return keys.map((k, idx) => {
    const t = maxTemp(groups.get(k));
    return isNum(t) ? { label: keys.length > 1 ? `GPU ${idx}` : 'GPU', value: t.toFixed(1) + '°C' } : null;
  }).filter(Boolean);
});

const nvmeTemps = computed(() => {
  const rows = thermalRows.value || [];
  const nvmeRows = rows.filter(r => (r.component || '') === 'NVMe');
  if (!nvmeRows.length) return [];
  const groups = new Map();
  for (const r of nvmeRows) {
    const gk = groupKeyFromSensorKey(r.sensorKey) || 'nvme';
    if (!groups.has(gk)) groups.set(gk, []);
    groups.get(gk).push(r);
  }
  const keys = Array.from(groups.keys()).sort();
  return keys.map((k, idx) => {
    const t = maxTemp(groups.get(k));
    return isNum(t) ? { label: keys.length > 1 ? `NVMe ${idx}` : 'NVMe', value: t.toFixed(1) + '°C' } : null;
  }).filter(Boolean);
});

// Thermal summary for collapsed thermal view (replaces vitals temp filter)
const thermalSummary = computed(() => {
  const out = [];
  const rows = thermalRows.value || [];

  const cpuRows = rows.filter(r => (r.component || '') === 'CPU');
  const cpuC = maxTemp(cpuRows);
  if (isNum(cpuC)) out.push({ key: 'cpuTemp', label: 'CPU Temp', value: cpuC.toFixed(1) + '°C' });

  const nvmeRows = rows.filter(r => (r.component || '') === 'NVMe');
  if (nvmeRows.length) {
    const groups = new Map();
    for (const r of nvmeRows) {
      const gk = groupKeyFromSensorKey(r.sensorKey) || 'nvme';
      if (!groups.has(gk)) groups.set(gk, []);
      groups.get(gk).push(r);
    }
    const keys = Array.from(groups.keys()).sort();
    keys.forEach((k, idx) => {
      const t = maxTemp(groups.get(k));
      if (isNum(t)) out.push({ key: 'nvme' + idx, label: keys.length > 1 ? 'NVMe ' + idx : 'NVMe', value: t.toFixed(1) + '°C' });
    });
  }

  const gpuRows = rows.filter(r => (r.component || '') === 'GPU');
  if (gpuRows.length) {
    const groups = new Map();
    for (const r of gpuRows) {
      const gk = groupKeyFromSensorKey(r.sensorKey) || 'gpu';
      if (!groups.has(gk)) groups.set(gk, []);
      groups.get(gk).push(r);
    }
    const keys = Array.from(groups.keys()).sort();
    keys.forEach((k, idx) => {
      const t = maxTemp(groups.get(k));
      if (isNum(t)) out.push({ key: 'gpu' + idx, label: keys.length > 1 ? 'GPU ' + idx : 'GPU', value: t.toFixed(1) + '°C' });
    });
  }
  return out;
});

function isNum(v) { return typeof v === 'number' && isFinite(v); }
function maxTemp(rows) {
  let m = null;
  for (const r of rows) {
    if (!isNum(r?.temperature)) continue;
    if (m == null || r.temperature > m) m = r.temperature;
  }
  return m;
}
function groupKeyFromSensorKey(k) {
  const s = String(k || '').toLowerCase();
  const m =
    s.match(/(gpu|card|nvme)\s*[-_]?(\d+)/i) ||
    s.match(/(gpu|card|nvme)(\d+)/i) ||
    s.match(/hwmon(\d+)/i);
  if (m) return (m[1] ? String(m[1]).toLowerCase() : 'hwmon') + (m[2] ?? m[1]);
  if (s.includes('nvidia')) return 'nvidia';
  if (s.includes('amdgpu') || s.includes('radeon')) return 'amd';
  return '';
}
function guessComponentFromKey(k) {
  const s = String(k || '').toLowerCase();
  if (!s) return '';
  if (s.startsWith('nvme')) return 'NVMe';
  if (s.includes('k10temp') || s.includes('coretemp') || s.includes('cpu')) return 'CPU';
  if (s.includes('amdgpu') || s.includes('radeon') || s.includes('nvidia') || s.includes('gpu')) return 'GPU';
  if (s.includes('acpitz') || s.includes('thermal_zone')) return 'ACPI';
  if (s.includes('pch') || s.includes('chipset')) return 'Chipset';
  return 'Other';
}
function guessNameFromKey(k) {
  let s = String(k || '').trim();
  if (!s) return '';
  s = s.replace(/^(k10temp_|coretemp_|acpitz_|amdgpu_|nvme_)/i, '');
  s = s.replace(/_/g, ' ');
  return s.length ? s[0].toUpperCase() + s.slice(1) : s;
}
</script>
