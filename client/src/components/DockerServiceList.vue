<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import { send, on as onWS } from '../lib/sharedWS.js';
import DockerServiceLogs from './DockerServiceLogs.vue';

const props = defineProps({
  clientId: { type: String, required: true },
  isConnected: { type: Boolean, default: false },
});

const services = ref([]);
const loading = ref(false);
const expandedService = ref(null);

function fetchServices() {
  if (!props.clientId) return;
  loading.value = true;
  send({ type: 'swarm_service_list_request', clientId: props.clientId });
}

const unsubs = [];
onMounted(() => {
  unsubs.push(onWS('swarm_service_list_response', (msg) => {
    if (msg.clientId === props.clientId) {
      services.value = msg.services || [];
      loading.value = false;
    }
  }));
  fetchServices();
});
onUnmounted(() => unsubs.forEach(fn => fn()));

function toggleLogs(svcId) {
  expandedService.value = expandedService.value === svcId ? null : svcId;
}

function formatPorts(ports) {
  if (!Array.isArray(ports) || !ports.length) return '-';
  return ports.map(p => {
    if (p.published) return `${p.published}:${p.target}/${p.protocol || 'tcp'}`;
    return `${p.target}/${p.protocol || 'tcp'}`;
  }).join(', ');
}

function replicaDisplay(svc) {
  if (svc.mode === 'global') return 'global';
  const running = svc.runningReplicas ?? svc.replicas ?? 0;
  const desired = svc.desiredReplicas ?? svc.replicas ?? 0;
  return `${running}/${desired}`;
}
</script>

<template>
  <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
    <div class="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Swarm Services</h3>
      <button
        @click="fetchServices"
        :disabled="!isConnected"
        class="px-2.5 py-1 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 disabled:opacity-40 transition-colors"
      >Refresh</button>
    </div>

    <div v-if="loading" class="p-4 text-center text-sm text-gray-500 dark:text-gray-400">Loading services...</div>

    <div v-else-if="!services.length" class="p-4 text-center text-sm text-gray-400 dark:text-gray-500">No swarm services found.</div>

    <table v-else class="w-full text-sm">
      <thead>
        <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Name</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Image</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Mode</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Replicas</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Ports</th>
          <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400"></th>
        </tr>
      </thead>
      <tbody>
        <template v-for="svc in services" :key="svc.id">
          <tr class="border-b border-gray-100 dark:border-gray-700/50 hover:bg-gray-50 dark:hover:bg-gray-750">
            <td class="px-4 py-2 font-medium text-gray-900 dark:text-gray-100">{{ svc.name }}</td>
            <td class="px-4 py-2 font-mono text-xs text-gray-500 dark:text-gray-400 truncate max-w-[200px]">{{ svc.image }}</td>
            <td class="px-4 py-2 text-gray-500 dark:text-gray-400">{{ svc.mode || 'replicated' }}</td>
            <td class="px-4 py-2 font-mono text-gray-900 dark:text-gray-100">{{ replicaDisplay(svc) }}</td>
            <td class="px-4 py-2 font-mono text-xs text-gray-500 dark:text-gray-400">{{ formatPorts(svc.ports) }}</td>
            <td class="px-4 py-2">
              <button
                @click="toggleLogs(svc.id)"
                class="text-xs text-blue-600 dark:text-blue-400 hover:underline"
              >{{ expandedService === svc.id ? 'Hide Logs' : 'Logs' }}</button>
            </td>
          </tr>
          <tr v-if="expandedService === svc.id">
            <td colspan="6" class="px-4 py-3 bg-gray-50 dark:bg-gray-900/50">
              <DockerServiceLogs :client-id="clientId" :service-id="svc.id" :service-name="svc.name" />
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>
