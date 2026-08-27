<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute } from 'vue-router';
import { fetchStack, fetchStackVersions, on as onWS } from '../lib/sharedWS.js';
import StackServiceCard from '../components/StackServiceCard.vue';

const route = useRoute();
const stackId = computed(() => String(route.params.stackId || ''));

const stack = ref(null);
const versions = ref([]);
const loading = ref(true);
const activeTab = ref('services');
const events = ref([]);

const tabs = ['services', 'networks', 'environment', 'deployments', 'events', 'versions'];

async function load() {
  loading.value = true;
  const [s, v] = await Promise.all([
    fetchStack(stackId.value),
    fetchStackVersions(stackId.value),
  ]);
  stack.value = s;
  versions.value = v;
  loading.value = false;
}

onMounted(load);
watch(stackId, load);

const statusDot = computed(() => {
  const map = { running: 'bg-green-500', deploying: 'bg-blue-500', stopped: 'bg-gray-400', failed: 'bg-red-500', degraded: 'bg-amber-500' };
  return map[stack.value?.status] || 'bg-gray-400';
});

const statusBg = computed(() => {
  const map = {
    running:   'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300',
    deploying: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300',
    stopped:   'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300',
    failed:    'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300',
    degraded:  'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300',
  };
  return map[stack.value?.status] || map.stopped;
});

const services = computed(() => stack.value?.services || []);
const publicServices = computed(() => services.value.filter(s => s.ports?.some(p => p.published)));
const internalServices = computed(() => services.value.filter(s => !s.ports?.some(p => p.published)));
const networks = computed(() => stack.value?.networks || []);
const activeVersionConfig = computed(() => {
  if (!stack.value?.activeVersion) return {};
  const v = versions.value.find(v => v.id === stack.value.activeVersion);
  return v?.config?.environment || stack.value?.environment || {};
});
const deployments = computed(() => stack.value?.deployments || []);

const unsubs = [];
onMounted(() => {
  unsubs.push(onWS('deploy_result', (msg) => {
    if (msg.stackId === stackId.value) load();
  }));
  unsubs.push(onWS('container_event', (msg) => {
    if (msg.stackId === stackId.value || msg.stackName === stack.value?.name) {
      events.value.push({
        ts: msg.ts || new Date().toISOString(),
        action: msg.action || msg.status || 'event',
        container: msg.containerName || msg.containerId || '',
        detail: msg.detail || '',
      });
      if (events.value.length > 200) events.value.splice(0, events.value.length - 200);
    }
  }));
});
onUnmounted(() => unsubs.forEach(fn => fn()));

function formatTs(ts) {
  try { return new Date(ts).toLocaleString(); } catch { return ts; }
}
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    <div v-if="loading" class="text-center py-12 text-gray-500 dark:text-gray-400 text-sm">Loading stack...</div>

    <template v-else-if="stack">
      <div class="flex items-center justify-between mb-4 flex-wrap gap-3">
        <div class="flex items-center gap-3">
          <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ stack.name }}</h1>
          <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium" :class="statusBg">
            <span class="w-1.5 h-1.5 rounded-full" :class="statusDot"></span>
            {{ stack.status || 'unknown' }}
          </span>
        </div>
        <!-- Deploy/Restart/Stop actions removed: stack lifecycle is managed by a
             separate tool. This view is read-only monitoring of stack state. -->
      </div>

      <div class="flex items-center gap-1 mb-4 border-b border-gray-200 dark:border-gray-700">
        <button
          v-for="tab in tabs"
          :key="tab"
          @click="activeTab = tab"
          class="px-3 py-2 text-xs font-medium capitalize transition-colors border-b-2 -mb-px"
          :class="activeTab === tab
            ? 'border-blue-500 text-blue-600 dark:text-blue-400'
            : 'border-transparent text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300'"
        >{{ tab }}</button>
      </div>

      <!-- Services Tab -->
      <div v-if="activeTab === 'services'" class="space-y-4">
        <div v-if="publicServices.length">
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Public Services</h3>
          <div class="space-y-3">
            <StackServiceCard v-for="svc in publicServices" :key="svc.name" :service="svc" :stack-name="stack.name" />
          </div>
        </div>
        <div v-if="internalServices.length">
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Internal Services</h3>
          <div class="space-y-3">
            <StackServiceCard v-for="svc in internalServices" :key="svc.name" :service="svc" :stack-name="stack.name" />
          </div>
        </div>
        <div v-if="!services.length" class="text-center py-8 text-sm text-gray-400 dark:text-gray-500">No services defined.</div>
      </div>

      <!-- Networks Tab -->
      <div v-if="activeTab === 'networks'">
        <div v-if="networks.length" class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Name</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Driver</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="net in networks" :key="net.name" class="border-b border-gray-100 dark:border-gray-700/50">
                <td class="px-4 py-2 font-mono text-gray-900 dark:text-gray-100">{{ net.name }}</td>
                <td class="px-4 py-2 text-gray-500 dark:text-gray-400">{{ net.driver || 'default' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-8 text-sm text-gray-400 dark:text-gray-500">No networks defined.</div>
      </div>

      <!-- Environment Tab -->
      <div v-if="activeTab === 'environment'">
        <div v-if="Object.keys(activeVersionConfig).length" class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Key</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Value</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(val, key) in activeVersionConfig" :key="key" class="border-b border-gray-100 dark:border-gray-700/50">
                <td class="px-4 py-2 font-mono text-gray-900 dark:text-gray-100">{{ key }}</td>
                <td class="px-4 py-2 font-mono text-gray-500 dark:text-gray-400 break-all">{{ val }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-8 text-sm text-gray-400 dark:text-gray-500">No environment variables.</div>
      </div>

      <!-- Deployments Tab -->
      <div v-if="activeTab === 'deployments'">
        <div v-if="deployments.length" class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Status</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Node</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Started</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Finished</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(dep, i) in deployments" :key="i" class="border-b border-gray-100 dark:border-gray-700/50">
                <td class="px-4 py-2">
                  <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium"
                    :class="dep.status === 'success' ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300'
                          : dep.status === 'failed' ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'
                          : 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'"
                  >{{ dep.status }}</span>
                </td>
                <td class="px-4 py-2 font-mono text-gray-900 dark:text-gray-100 text-xs">{{ dep.node || '-' }}</td>
                <td class="px-4 py-2 text-gray-500 dark:text-gray-400 text-xs">{{ formatTs(dep.startedAt) }}</td>
                <td class="px-4 py-2 text-gray-500 dark:text-gray-400 text-xs">{{ dep.finishedAt ? formatTs(dep.finishedAt) : '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-8 text-sm text-gray-400 dark:text-gray-500">No deployment history.</div>
      </div>

      <!-- Events Tab -->
      <div v-if="activeTab === 'events'">
        <div v-if="events.length" class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden max-h-96 overflow-y-auto">
          <table class="w-full text-xs font-mono">
            <thead class="sticky top-0 bg-white dark:bg-gray-800">
              <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Time</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Action</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Container</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Detail</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(ev, i) in events" :key="i" class="border-b border-gray-100 dark:border-gray-700/50">
                <td class="px-4 py-1.5 text-gray-500 dark:text-gray-400 whitespace-nowrap">{{ formatTs(ev.ts) }}</td>
                <td class="px-4 py-1.5 text-gray-900 dark:text-gray-100">{{ ev.action }}</td>
                <td class="px-4 py-1.5 text-gray-500 dark:text-gray-400 truncate max-w-[160px]">{{ ev.container }}</td>
                <td class="px-4 py-1.5 text-gray-500 dark:text-gray-400 truncate max-w-[200px]">{{ ev.detail }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-8 text-sm text-gray-400 dark:text-gray-500">No container events yet. Events will appear in real-time.</div>
      </div>

      <!-- Versions Tab -->
      <div v-if="activeTab === 'versions'">
        <div v-if="versions.length" class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
          <table class="w-full text-sm">
            <thead>
              <tr class="border-b border-gray-200 dark:border-gray-700 text-left">
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Version</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Created</th>
                <th class="px-4 py-2 text-xs font-medium text-gray-500 dark:text-gray-400">Active</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="v in versions" :key="v.id" class="border-b border-gray-100 dark:border-gray-700/50">
                <td class="px-4 py-2 font-mono text-gray-900 dark:text-gray-100">{{ v.version || v.id }}</td>
                <td class="px-4 py-2 text-gray-500 dark:text-gray-400 text-xs">{{ formatTs(v.createdAt) }}</td>
                <td class="px-4 py-2">
                  <span v-if="v.id === stack.activeVersion" class="w-2 h-2 rounded-full bg-green-500 inline-block"></span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-8 text-sm text-gray-400 dark:text-gray-500">No versions recorded.</div>
      </div>
    </template>

    <div v-else class="text-center py-12 text-gray-400 dark:text-gray-500 text-sm">Stack not found.</div>
  </main>
</template>
