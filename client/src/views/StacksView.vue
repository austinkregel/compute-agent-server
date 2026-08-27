<script setup>
import { ref, computed, onMounted } from 'vue';
import { fetchStacks } from '../lib/sharedWS.js';
import StackImportWizard from '../components/StackImportWizard.vue';

const stacks = ref([]);
const loading = ref(true);
const search = ref('');
const showImport = ref(false);

async function loadStacks() {
  loading.value = true;
  stacks.value = await fetchStacks();
  loading.value = false;
}

onMounted(loadStacks);

const filtered = computed(() => {
  const q = search.value.toLowerCase().trim();
  if (!q) return stacks.value;
  return stacks.value.filter(s =>
    (s.name || '').toLowerCase().includes(q) ||
    (s.description || '').toLowerCase().includes(q)
  );
});

const statusConfig = {
  running:   { dot: 'bg-green-500',  bg: 'bg-green-100 dark:bg-green-900/30',  text: 'text-green-700 dark:text-green-300' },
  deploying: { dot: 'bg-blue-500',   bg: 'bg-blue-100 dark:bg-blue-900/30',    text: 'text-blue-700 dark:text-blue-300' },
  stopped:   { dot: 'bg-gray-400',   bg: 'bg-gray-100 dark:bg-gray-700',       text: 'text-gray-600 dark:text-gray-300' },
  failed:    { dot: 'bg-red-500',    bg: 'bg-red-100 dark:bg-red-900/30',      text: 'text-red-700 dark:text-red-300' },
  degraded:  { dot: 'bg-amber-500',  bg: 'bg-amber-100 dark:bg-amber-900/30',  text: 'text-amber-700 dark:text-amber-300' },
};

function getStatusCfg(status) {
  return statusConfig[status] || statusConfig.stopped;
}
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Stacks</h1>
      <div class="flex items-center gap-2">
        <button
          @click="loadStacks"
          class="px-3 py-1.5 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
        >Refresh</button>
        <button
          @click="showImport = true"
          class="px-3 py-1.5 rounded text-xs font-medium bg-blue-600 text-white hover:bg-blue-700 transition-colors"
        >Import from Compose</button>
      </div>
    </div>

    <div class="mb-4">
      <input
        v-model="search"
        type="text"
        placeholder="Filter stacks..."
        class="w-full max-w-sm px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
      />
    </div>

    <div v-if="loading" class="text-center py-12 text-gray-500 dark:text-gray-400 text-sm">Loading stacks...</div>

    <div v-else-if="!filtered.length" class="text-center py-12">
      <div class="text-gray-400 dark:text-gray-500 text-sm">{{ search ? 'No stacks match your filter.' : 'No stacks configured yet.' }}</div>
    </div>

    <div v-else class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      <router-link
        v-for="stack in filtered"
        :key="stack.id"
        :to="`/stacks/${encodeURIComponent(stack.id)}`"
        class="rounded-lg bg-white dark:bg-gray-800 p-4 hover:ring-2 hover:ring-blue-400 dark:hover:ring-blue-500 transition-shadow block"
      >
        <div class="flex items-center justify-between mb-2">
          <span class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{{ stack.name }}</span>
          <span
            class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium"
            :class="[getStatusCfg(stack.status).bg, getStatusCfg(stack.status).text]"
          >
            <span class="w-1.5 h-1.5 rounded-full" :class="getStatusCfg(stack.status).dot"></span>
            {{ stack.status || 'unknown' }}
          </span>
        </div>
        <p v-if="stack.description" class="text-xs text-gray-500 dark:text-gray-400 mb-3 line-clamp-2">{{ stack.description }}</p>
        <div class="flex items-center gap-3 text-xs text-gray-500 dark:text-gray-400">
          <span v-if="stack.serviceCount != null">{{ stack.serviceCount }} service{{ stack.serviceCount !== 1 ? 's' : '' }}</span>
          <span v-if="stack.node" class="font-mono truncate max-w-[120px]">{{ stack.node }}</span>
          <span v-if="stack.version" class="ml-auto font-mono">v{{ stack.version }}</span>
        </div>
      </router-link>
    </div>

    <StackImportWizard v-model="showImport" />
  </main>
</template>
