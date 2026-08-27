<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import { send, on as onWS } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, required: true },
  isConnected: { type: Boolean, default: false },
});

const networks = ref([]);
const loading = ref(false);

function fetchNetworks() {
  if (!props.clientId) return;
  loading.value = true;
  send({ type: 'swarm_network_list_request', clientId: props.clientId });
}

const unsubs = [];
onMounted(() => {
  unsubs.push(onWS('swarm_network_list_response', (msg) => {
    if (msg.clientId === props.clientId) {
      networks.value = msg.networks || [];
      loading.value = false;
    }
  }));
  fetchNetworks();
});
onUnmounted(() => unsubs.forEach(fn => fn()));

function truncateId(id) {
  return id ? id.substring(0, 12) : '-';
}
</script>

<template>
  <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
    <div class="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Docker Networks</h3>
      <button
        @click="fetchNetworks"
        :disabled="!isConnected"
        class="px-2.5 py-1 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 disabled:opacity-40 transition-colors"
      >Refresh</button>
    </div>

    <div v-if="loading" class="p-4 text-center text-sm text-gray-500 dark:text-gray-400">Loading networks...</div>

    <div v-else-if="!networks.length" class="p-4 text-center text-sm text-gray-400 dark:text-gray-500">No networks found.</div>

    <table v-else class="w-full text-sm">
      <thead>
        <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Name</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Driver</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Scope</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">ID</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="net in networks" :key="net.id" class="border-b border-gray-100 dark:border-gray-700/50">
          <td class="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">{{ net.name }}</td>
          <td class="px-4 py-2 text-gray-500 dark:text-gray-400">{{ net.driver }}</td>
          <td class="px-4 py-2 text-gray-500 dark:text-gray-400">{{ net.scope }}</td>
          <td class="px-4 py-2 font-mono text-xs text-gray-400 dark:text-gray-500">{{ truncateId(net.id) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
