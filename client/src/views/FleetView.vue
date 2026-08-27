<script setup>
import { ref, computed, onMounted } from 'vue';
import { clientIds, statsMap, dockerStatusMap, fetchSwarmClusters } from '../lib/sharedWS.js';

const clusters = ref([]);
const loading = ref(true);

onMounted(async () => {
  clusters.value = await fetchSwarmClusters();
  loading.value = false;
});

const onlineCount = computed(() => clientIds.value.filter(c => c.connected !== false).length);
const dockerEnabledCount = computed(() => {
  return clientIds.value.filter(c => {
    const d = dockerStatusMap[c.clientId];
    return d && d.available;
  }).length;
});

const managerAddrMap = computed(() => {
  const map = {};
  for (const cluster of clusters.value) {
    for (const node of (cluster.managers || [])) {
      if (node.managerAddr) {
        map[node.managerAddr] = cluster.id || cluster.clusterId;
      }
    }
  }
  return map;
});

const clusterGroups = computed(() => {
  const groups = [];
  const assignedIds = new Set();

  for (const cluster of clusters.value) {
    const managers = (cluster.managers || []).map(n => n.clientId);
    const workers = (cluster.workers || []).map(n => n.clientId);

    const workersByAddr = clientIds.value.filter(c => {
      if (assignedIds.has(c.clientId) || managers.includes(c.clientId) || workers.includes(c.clientId)) return false;
      const d = dockerStatusMap[c.clientId];
      if (!d?.swarm?.managerAddr) return false;
      return managerAddrMap.value[d.swarm.managerAddr] === (cluster.id || cluster.clusterId);
    }).map(c => c.clientId);

    const allWorkers = [...workers, ...workersByAddr];
    managers.forEach(id => assignedIds.add(id));
    allWorkers.forEach(id => assignedIds.add(id));

    groups.push({
      name: cluster.name || cluster.id || cluster.clusterId || 'Unnamed Cluster',
      id: cluster.id || cluster.clusterId,
      managers,
      workers: allWorkers,
    });
  }

  return groups;
});

const standaloneNodes = computed(() => {
  const assigned = new Set();
  for (const g of clusterGroups.value) {
    g.managers.forEach(id => assigned.add(id));
    g.workers.forEach(id => assigned.add(id));
  }
  return clientIds.value.filter(c => !assigned.has(c.clientId));
});

function getStats(clientId) {
  return statsMap[clientId] || {};
}

function getDocker(clientId) {
  return dockerStatusMap[clientId] || {};
}

function isOnline(clientId) {
  const entry = clientIds.value.find(c => c.clientId === clientId);
  return entry?.connected !== false;
}

function cpuDisplay(stats) {
  const load = stats?.load?.['1m'];
  if (typeof load === 'number') return load.toFixed(2);
  if (typeof stats?.cpu === 'number') return stats.cpu.toFixed(1) + '%';
  return '-';
}

function memDisplay(stats) {
  if (!stats?.mem?.total) return '-';
  return ((stats.mem.used / stats.mem.total) * 100).toFixed(0) + '%';
}
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">Fleet Overview</h1>

    <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
      <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
        <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Total Nodes</div>
        <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{{ clientIds.length }}</div>
      </div>
      <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
        <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Online</div>
        <div class="text-2xl font-bold text-green-600 dark:text-green-400">{{ onlineCount }}</div>
      </div>
      <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
        <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Swarm Clusters</div>
        <div class="text-2xl font-bold text-blue-600 dark:text-blue-400">{{ clusterGroups.length }}</div>
      </div>
      <div class="rounded-lg bg-white dark:bg-gray-800 p-5">
        <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-1">Docker Enabled</div>
        <div class="text-2xl font-bold text-purple-600 dark:text-purple-400">{{ dockerEnabledCount }}</div>
      </div>
    </div>

    <div v-if="loading" class="text-center py-12 text-gray-500 dark:text-gray-400 text-sm">Loading fleet data...</div>

    <template v-else>
      <div v-for="group in clusterGroups" :key="group.id" class="mb-6">
        <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center gap-2">
          <svg class="w-4 h-4 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" /></svg>
          {{ group.name }}
          <span class="text-xs font-normal text-gray-400 dark:text-gray-500">{{ group.managers.length + group.workers.length }} nodes</span>
        </h2>

        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          <router-link
            v-for="cid in group.managers"
            :key="cid"
            :to="`/client/${encodeURIComponent(cid)}`"
            class="rounded-lg bg-white dark:bg-gray-800 p-4 hover:ring-2 hover:ring-blue-400 dark:hover:ring-blue-500 transition-shadow block"
          >
            <div class="flex items-center justify-between mb-2">
              <div class="flex items-center gap-2 min-w-0">
                <span class="w-2 h-2 rounded-full flex-shrink-0" :class="isOnline(cid) ? 'bg-green-500' : 'bg-gray-400'"></span>
                <span class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ getStats(cid).hostname || cid }}</span>
              </div>
              <div class="flex items-center gap-1.5 flex-shrink-0">
                <span class="px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">Manager</span>
                <span v-if="getDocker(cid).available" class="px-1.5 py-0.5 rounded text-[10px] font-medium bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">Docker</span>
              </div>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 truncate">{{ getStats(cid).platform }} {{ getStats(cid).arch }}</div>
            <div class="flex gap-4 mt-2 text-xs text-gray-500 dark:text-gray-400">
              <span>CPU: <span class="font-mono">{{ cpuDisplay(getStats(cid)) }}</span></span>
              <span>Mem: <span class="font-mono">{{ memDisplay(getStats(cid)) }}</span></span>
            </div>
          </router-link>

          <router-link
            v-for="cid in group.workers"
            :key="cid"
            :to="`/client/${encodeURIComponent(cid)}`"
            class="rounded-lg bg-white dark:bg-gray-800 p-4 hover:ring-2 hover:ring-blue-400 dark:hover:ring-blue-500 transition-shadow block"
          >
            <div class="flex items-center justify-between mb-2">
              <div class="flex items-center gap-2 min-w-0">
                <span class="w-2 h-2 rounded-full flex-shrink-0" :class="isOnline(cid) ? 'bg-green-500' : 'bg-gray-400'"></span>
                <span class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ getStats(cid).hostname || cid }}</span>
              </div>
              <div class="flex items-center gap-1.5 flex-shrink-0">
                <span class="px-1.5 py-0.5 rounded text-[10px] font-medium bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300">Worker</span>
                <span v-if="getDocker(cid).available" class="px-1.5 py-0.5 rounded text-[10px] font-medium bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">Docker</span>
              </div>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 truncate">{{ getStats(cid).platform }} {{ getStats(cid).arch }}</div>
            <div class="flex gap-4 mt-2 text-xs text-gray-500 dark:text-gray-400">
              <span>CPU: <span class="font-mono">{{ cpuDisplay(getStats(cid)) }}</span></span>
              <span>Mem: <span class="font-mono">{{ memDisplay(getStats(cid)) }}</span></span>
            </div>
          </router-link>
        </div>
      </div>

      <div v-if="standaloneNodes.length" class="mb-6">
        <h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-3 flex items-center gap-2">
          <svg class="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" /></svg>
          Standalone Nodes
          <span class="text-xs font-normal text-gray-400 dark:text-gray-500">{{ standaloneNodes.length }} nodes</span>
        </h2>

        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          <router-link
            v-for="node in standaloneNodes"
            :key="node.clientId"
            :to="`/client/${encodeURIComponent(node.clientId)}`"
            class="rounded-lg bg-white dark:bg-gray-800 p-4 hover:ring-2 hover:ring-blue-400 dark:hover:ring-blue-500 transition-shadow block"
          >
            <div class="flex items-center justify-between mb-2">
              <div class="flex items-center gap-2 min-w-0">
                <span class="w-2 h-2 rounded-full flex-shrink-0" :class="node.connected !== false ? 'bg-green-500' : 'bg-gray-400'"></span>
                <span class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ getStats(node.clientId).hostname || node.clientId }}</span>
              </div>
              <span v-if="getDocker(node.clientId).available" class="px-1.5 py-0.5 rounded text-[10px] font-medium bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">Docker</span>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 truncate">{{ getStats(node.clientId).platform }} {{ getStats(node.clientId).arch }}</div>
            <div class="flex gap-4 mt-2 text-xs text-gray-500 dark:text-gray-400">
              <span>CPU: <span class="font-mono">{{ cpuDisplay(getStats(node.clientId)) }}</span></span>
              <span>Mem: <span class="font-mono">{{ memDisplay(getStats(node.clientId)) }}</span></span>
            </div>
          </router-link>
        </div>
      </div>

      <div v-if="!clusterGroups.length && !standaloneNodes.length" class="text-center py-12">
        <div class="text-gray-400 dark:text-gray-500 text-sm">No nodes connected</div>
      </div>
    </template>
  </main>
</template>
