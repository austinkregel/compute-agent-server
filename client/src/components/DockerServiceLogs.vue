<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue';
import { send, on as onWS } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, required: true },
  serviceId: { type: String, required: true },
  serviceName: { type: String, default: '' },
});

const logs = ref('');
const loading = ref(false);
const logsContainer = ref(null);

function fetchLogs() {
  if (!props.clientId || !props.serviceId) return;
  loading.value = true;
  logs.value = '';
  send({
    type: 'swarm_service_logs_request',
    clientId: props.clientId,
    serviceId: props.serviceId,
    tail: 200,
  });
}

const unsubs = [];
onMounted(() => {
  unsubs.push(onWS('swarm_service_logs_response', (msg) => {
    if (msg.clientId === props.clientId && msg.serviceId === props.serviceId) {
      logs.value = msg.logs || msg.data || '';
      loading.value = false;
      nextTick(() => {
        if (logsContainer.value) {
          logsContainer.value.scrollTop = logsContainer.value.scrollHeight;
        }
      });
    }
  }));
  fetchLogs();
});
onUnmounted(() => unsubs.forEach(fn => fn()));
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <span class="text-xs font-medium text-gray-700 dark:text-gray-300">
        Logs: {{ serviceName || serviceId }}
      </span>
      <button
        @click="fetchLogs"
        class="px-2 py-1 rounded text-[10px] font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
      >Refresh</button>
    </div>

    <div v-if="loading" class="text-center py-4 text-xs text-gray-500 dark:text-gray-400">Loading logs...</div>

    <div
      v-else
      ref="logsContainer"
      class="bg-gray-900 dark:bg-black rounded-lg p-3 max-h-64 overflow-y-auto"
    >
      <pre class="text-xs text-green-400 font-mono whitespace-pre-wrap break-all">{{ logs || 'No logs available.' }}</pre>
    </div>
  </div>
</template>
