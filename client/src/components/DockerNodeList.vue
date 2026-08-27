<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import { send, on as onWS } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, required: true },
  isConnected: { type: Boolean, default: false },
});

const nodes = ref([]);
const loading = ref(false);

function fetchNodes() {
  if (!props.clientId) return;
  loading.value = true;
  send({ type: 'swarm_node_list_request', clientId: props.clientId });
}

const unsubs = [];
onMounted(() => {
  unsubs.push(onWS('swarm_node_list_response', (msg) => {
    if (msg.clientId === props.clientId) {
      nodes.value = msg.nodes || [];
      loading.value = false;
    }
  }));
  fetchNodes();
});
onUnmounted(() => unsubs.forEach(fn => fn()));

function availabilityColor(avail) {
  if (avail === 'active') return 'text-green-600 dark:text-green-400';
  if (avail === 'pause') return 'text-yellow-600 dark:text-yellow-400';
  if (avail === 'drain') return 'text-red-600 dark:text-red-400';
  return 'text-gray-500 dark:text-gray-400';
}

function statusDot(status) {
  if (status === 'ready') return 'bg-green-500';
  if (status === 'down') return 'bg-red-500';
  return 'bg-gray-400';
}
</script>

<template>
  <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
    <div class="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Swarm Nodes</h3>
      <button
        @click="fetchNodes"
        :disabled="!isConnected"
        class="px-2.5 py-1 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 disabled:opacity-40 transition-colors"
      >Refresh</button>
    </div>

    <div v-if="loading" class="p-4 text-center text-sm text-gray-500 dark:text-gray-400">Loading nodes...</div>

    <div v-else-if="!nodes.length" class="p-4 text-center text-sm text-gray-400 dark:text-gray-500">No swarm nodes found.</div>

    <table v-else class="w-full text-sm">
      <thead>
        <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Hostname</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Role</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Availability</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Status</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Address</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Engine</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="node in nodes" :key="node.id" class="border-b border-gray-100 dark:border-gray-700/50">
          <td class="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">{{ node.hostname }}</td>
          <td class="px-4 py-2">
            <span
              class="px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase"
              :class="node.role === 'manager'
                ? 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300'
                : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300'"
            >{{ node.role }}</span>
          </td>
          <td class="px-4 py-2 font-medium" :class="availabilityColor(node.availability)">{{ node.availability }}</td>
          <td class="px-4 py-2">
            <span class="inline-flex items-center gap-1">
              <span class="w-1.5 h-1.5 rounded-full" :class="statusDot(node.status)"></span>
              <span class="text-gray-500 dark:text-gray-400">{{ node.status }}</span>
            </span>
          </td>
          <td class="px-4 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ node.addr || '-' }}</td>
          <td class="px-4 py-2 text-xs text-gray-500 dark:text-gray-400">{{ node.engineVersion || '-' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
