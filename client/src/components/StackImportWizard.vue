<script setup>
import { ref, computed } from 'vue';
import { clientIds, importComposeScan, importComposeParse } from '../lib/sharedWS.js';

const props = defineProps({
  modelValue: { type: Boolean, default: false },
});
const emit = defineEmits(['update:modelValue']);

const step = ref(1);
const selectedAgent = ref('');
const directory = ref('');
const scanning = ref(false);
const scanResults = ref([]);
const importBusy = ref(false);
const importResults = ref(null);

const connectedClients = computed(() =>
  clientIds.value.filter(c => c.connected !== false)
);

function close() {
  emit('update:modelValue', false);
  step.value = 1;
  selectedAgent.value = '';
  directory.value = '';
  scanResults.value = [];
  importResults.value = null;
}

async function scan() {
  if (!selectedAgent.value || !directory.value.trim()) return;
  scanning.value = true;
  try {
    const res = await importComposeScan(selectedAgent.value, directory.value.trim());
    // Agent now returns files shaped as { file, path, size }. Derive a default
    // stack name from the file's parent directory (or the file name).
    scanResults.value = (res.files || []).map(f => {
      const rel = f.file || f.path || '';
      const parts = rel.split('/').filter(Boolean);
      const dir = parts.length > 1 ? parts[parts.length - 2] : '';
      return {
        ...f,
        selected: true,
        stackName: dir || rel.replace(/\.(ya?ml)$/i, '') || '',
      };
    });
    step.value = 2;
  } catch {}
  scanning.value = false;
}

async function doImport() {
  const selections = scanResults.value
    .filter(f => f.selected)
    .map(f => ({ path: f.path, stackName: f.stackName }));
  if (!selections.length) return;
  importBusy.value = true;
  try {
    const res = await importComposeParse(selectedAgent.value, selections);
    importResults.value = res;
    step.value = 3;
  } catch (e) {
    importResults.value = { error: String(e) };
    step.value = 3;
  }
  importBusy.value = false;
}

const hasSelected = computed(() => scanResults.value.some(f => f.selected));
</script>

<template>
  <Teleport to="body">
    <div v-if="modelValue" class="fixed inset-0 z-50 flex items-center justify-center">
      <div class="absolute inset-0 bg-black/50" @click="close"></div>
      <div class="relative bg-white dark:bg-gray-800 rounded-lg shadow-xl w-full max-w-lg mx-4 max-h-[80vh] flex flex-col">
        <div class="flex items-center justify-between p-4 border-b border-gray-200 dark:border-gray-700">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Import from Docker Compose</h2>
          <button @click="close" class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
          </button>
        </div>

        <div class="p-4 overflow-y-auto flex-1">
          <!-- Step indicator -->
          <div class="flex items-center gap-2 mb-4">
            <span v-for="s in 3" :key="s"
              class="w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-bold"
              :class="s === step ? 'bg-blue-600 text-white' : s < step ? 'bg-green-500 text-white' : 'bg-gray-200 dark:bg-gray-700 text-gray-500 dark:text-gray-400'"
            >{{ s }}</span>
          </div>

          <!-- Step 1: Select agent and directory -->
          <div v-if="step === 1">
            <div class="space-y-3">
              <div>
                <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Agent Node</label>
                <select
                  v-model="selectedAgent"
                  class="w-full px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="">Select a node...</option>
                  <option v-for="c in connectedClients" :key="c.clientId" :value="c.clientId">{{ c.clientId }}</option>
                </select>
              </div>
              <div>
                <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Directory Path</label>
                <input
                  v-model="directory"
                  type="text"
                  placeholder="/opt/docker or /home/user/projects"
                  class="w-full px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
            </div>
            <div class="mt-4 flex justify-end">
              <button
                @click="scan"
                :disabled="!selectedAgent || !directory.trim() || scanning"
                class="px-3 py-1.5 rounded text-xs font-medium bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-40 transition-colors"
              >{{ scanning ? 'Scanning...' : 'Scan Directory' }}</button>
            </div>
          </div>

          <!-- Step 2: Scan results -->
          <div v-if="step === 2">
            <div v-if="!scanResults.length" class="text-center py-6 text-sm text-gray-400 dark:text-gray-500">
              No compose files found in that directory.
            </div>
            <div v-else class="space-y-2">
              <div
                v-for="(file, idx) in scanResults"
                :key="idx"
                class="flex items-center gap-3 p-2 rounded border border-gray-200 dark:border-gray-700"
              >
                <input type="checkbox" v-model="file.selected" class="rounded border-gray-300 dark:border-gray-600" />
                <div class="flex-1 min-w-0">
                  <div class="text-xs font-mono text-gray-500 dark:text-gray-400 truncate">{{ file.path }}</div>
                  <input
                    v-model="file.stackName"
                    type="text"
                    placeholder="Stack name"
                    class="mt-1 w-full px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500"
                  />
                </div>
              </div>
            </div>
            <div class="mt-4 flex justify-between">
              <button @click="step = 1" class="px-3 py-1.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors">Back</button>
              <button
                @click="doImport"
                :disabled="!hasSelected || importBusy"
                class="px-3 py-1.5 rounded text-xs font-medium bg-green-600 text-white hover:bg-green-700 disabled:opacity-40 transition-colors"
              >{{ importBusy ? 'Importing...' : 'Import Selected' }}</button>
            </div>
          </div>

          <!-- Step 3: Results -->
          <div v-if="step === 3">
            <div v-if="importResults?.error" class="rounded-lg bg-red-50 dark:bg-red-900/20 p-4 text-sm text-red-700 dark:text-red-300">
              {{ importResults.error }}
            </div>
            <div v-else class="space-y-2">
              <div class="text-sm text-green-600 dark:text-green-400 font-medium mb-2">Import complete!</div>
              <div v-for="(result, idx) in (importResults?.results || [])" :key="idx"
                class="flex items-center gap-2 text-xs"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="result.ok ? 'bg-green-500' : 'bg-red-500'"></span>
                <span class="text-gray-900 dark:text-gray-100">{{ result.stackName || result.name }}</span>
                <span v-if="result.error" class="text-red-500 dark:text-red-400">{{ result.error }}</span>
              </div>
            </div>
            <div class="mt-4 flex justify-end">
              <button @click="close" class="px-3 py-1.5 rounded text-xs font-medium bg-blue-600 text-white hover:bg-blue-700 transition-colors">Done</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
