<script setup>
import { clientIds, statsMap, connected } from '../lib/sharedWS.js';
// CertsStatusBadge removed
import { computed } from 'vue';
import { RouterLink } from 'vue-router';

import ClientStatusBadge from './ClientStatusBadge.vue';
import CpuSparkline from './CpuSparkline.vue';
import MemorySparkline from './MemorySparkline.vue';
import OSAvatar from './OSAvatar.vue';
import { statsHistory } from '../lib/sharedWS.js';

// Compute client dashboard route
function clientRoute(clientId) {
  return `/client/${encodeURIComponent(clientId)}`;
}

// Compute a list of client info objects
const ONLINE_THRESHOLD_MS = 300000; // 5 minutes
const maxPoints = 15;
const tail = (arr) => arr.slice(-maxPoints);
const clients = computed(() => {
  const now = Date.now();
  return clientIds.value.map(({clientId: id, lastPong: ts, ...otherdata}) => {
    const stats = statsMap[id] || {
      ts,
    };
    const history = statsHistory[id] || [];
    const platform = stats.platform || stats.os || stats.platformId || otherdata.platform || '';
    const release = stats.release || stats.platformVersion || '';
    const hostname = stats.hostname || otherdata.hostname || '';
    const arch = stats.arch || otherdata.arch || '';
    const cpus = stats.cpus ?? null;

    // CPU: prefer stats.cpu (percent), else stats.load?.['1m'] (load avg)
    const cpuPct = (typeof stats.cpu === 'number') ? stats.cpu : null;
    const cpuLoad1m = (stats.load && typeof stats.load['1m'] === 'number') ? stats.load['1m'] : null;
    // Memory: percent used
    const memPct = (stats.mem && typeof stats.mem.used === 'number' && typeof stats.mem.total === 'number' && stats.mem.total > 0)
      ? ((stats.mem.used / stats.mem.total) * 100)
      : null;
    return {
      id,
      platform,
      release,
      hostname,
      arch,
      cpus,
      cpuPct,
      cpuLoad1m,
      memPct,
      name: stats.name ?? id,
      version: stats.agentVersion || stats?.agent?.version || otherdata.agentVersion || '-',
      ts: stats.ts,
      // Undefined for callers/servers that don't report it; only an explicit
      // false means "socket is down".
      connected: otherdata.connected,
      updates: stats.updates || null,
      restartRequired: !!stats?.updates?.restartRequired,
      cpuSeries: tail(history).map(s => s.load?.['1m'] ?? 0),
      memSeries: tail(history).map(s => {
        const m = s.mem; return m ? (m.used / m.total) * 100 : 0;
      }),
    };
  });
});
</script>

<template>
  <div class="max-w-7xl mx-auto mt-8 px-4 sm:px-6 lg:px-8">
    <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-4">Clients</h2>
    <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
      <thead>
        <tr>
          <th class="px-4 py-2 text-left">Client</th>
          <th class="px-4 py-2 text-left"></th>
          <th class="px-4 py-2 text-left"></th>
          <th class="px-4 py-2 text-left">Agent</th>
          <th class="px-4 py-2 text-left">Updates</th>
          <th class="px-4 py-2 text-left"></th>
          <th class="px-4 py-2 text-left"></th>
          <!-- Certs column removed -->
        </tr>
      </thead>
      <tbody>
        <tr v-for="client in clients" :key="client.id" class="hover:bg-gray-100 dark:hover:bg-gray-700 border-b border-gray-200 dark:border-gray-700">
            <td class="px-4 py-2">
              <div class="flex items-start gap-3 min-w-0">
                <OSAvatar
                  :platform="client.platform"
                  :release="client.release"
                  :size="32"
                  :title="client.release ? `${client.platform || 'linux'} ${client.release}` : (client.platform || 'Unknown OS')"
                />

                <div class="min-w-0 flex-1">
                  <RouterLink
                    :to="clientRoute(client.id)"
                    class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate hover:text-indigo-600 dark:hover:text-indigo-400 hover:underline cursor-pointer block"
                    :title="`Open ${client.name}`"
                  >
                    {{ client.name }}
                  </RouterLink>
                  <div class="text-xs text-gray-500 dark:text-gray-400 font-mono truncate">
                    <span>{{ client.hostname || client.id }}</span>
                    <span v-if="client.platform" class="mx-1">·</span>
                    <span v-if="client.platform">{{ client.platform }}</span><span v-if="client.release"> {{ client.release }}</span>
                    <span v-if="client.arch" class="mx-1">·</span>
                    <span v-if="client.arch">{{ client.arch }}</span>
                    <span v-if="client.cpus != null" class="mx-1">·</span>
                    <span v-if="client.cpus != null">{{ client.cpus }} CPU</span>
                  </div>
                </div>
              </div>
            </td>


            <td class="px-4 py-2">
              <div class="flex flex-col items-start gap-1">
                <div class="flex items-center gap-2 flex-wrap">
                    <CpuSparkline :data="client.cpuSeries" :width="96" :height="14" :label="`CPU history for ${client.name}`" />
                  </div>
                  <div class="text-xs text-gray-500 dark:text-gray-400 truncate">CPU Usage</div>
                </div>
            </td>
            <td class="px-4 py-2">
              <div class="flex flex-col items-start gap-1">
                <div class="flex items-center gap-2 flex-wrap">
                    <MemorySparkline :data="client.memSeries" :width="96" :height="14" :min="0" :max="100" :label="`Memory history for ${client.name}`" />
                </div>
                <div class="text-xs text-gray-500 dark:text-gray-400 truncate">Memory Usage</div>
              </div>
            </td>
            <td class="px-4 py-2">
              <div class="text-sm font-mono text-gray-900 dark:text-gray-100 truncate">{{ client.version }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400 truncate">agent</div>
            </td>

            <td class="px-4 py-2">
              <div class="flex flex-col items-start gap-1">
                <span
                  class="inline-flex items-center gap-2 text-xs font-mono px-2 py-1 rounded border"
                  :class="(client.updates && typeof client.updates.available === 'number' && client.updates.available > 0)
                    ? 'border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300'
                    : 'border-green-200 dark:border-green-800 bg-green-50 dark:bg-green-900/20 text-green-700 dark:text-green-300'"
                  :title="client.updates?.checkError ? ('Update check failed: ' + client.updates.checkError) : (client.updates?.lastChecked || '')"
                >
                  <span v-if="client.updates && typeof client.updates.available === 'number'">{{ client.updates.available }}</span>
                  <span v-else>-</span>
                  <span class="text-gray-500 dark:text-gray-400">updates</span>
                  <span v-if="client.restartRequired" class="text-orange-700 dark:text-orange-300" title="Reboot required">reboot</span>
                </span>
                <div v-if="client.updates?.security != null" class="text-xs text-gray-500 dark:text-gray-400 font-mono">
                  security: {{ client.updates.security }}
                </div>
                <div v-else class="text-xs text-gray-500 dark:text-gray-400"> </div>
              </div>
            </td>
            <td class="px-4 py-2">
              <ClientStatusBadge :client="client" variant="dot" />
            </td>
            <td class="px-4 py-2">
              <RouterLink
                :to="clientRoute(client.id)"
                class="inline-flex items-center px-2 py-1 text-xs font-medium rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-600 hover:border-indigo-400 dark:hover:border-indigo-500 transition-colors"
                title="Open client dashboard"
              >
                Open
              </RouterLink>
            </td>
          </tr>
        <tr v-if="clients.length === 0">
          <td colspan="7" class="px-4 py-4 text-center text-gray-500">No clients connected.</td>
        </tr>
      </tbody>
      </table>
    </div>
    </div>
  </div>
</template>
