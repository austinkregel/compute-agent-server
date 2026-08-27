<script setup>
import { ref, computed, onMounted } from 'vue';

const props = defineProps({
  clientId: { type: String, required: true },
  isConnected: { type: Boolean, default: false },
});

const containers = ref([]);
const loading = ref(false);
const search = ref('');
const filterSource = ref('all');
const filterState = ref('all');

async function fetchContainers() {
  if (!props.clientId) return;
  loading.value = true;
  try {
    const res = await fetch(`/api/client/${encodeURIComponent(props.clientId)}/containers`, {
      credentials: 'include',
      headers: { 'Accept': 'application/json' },
    });
    if (res.ok) {
      const json = await res.json();
      containers.value = json.containers || [];
    }
  } catch {}
  loading.value = false;
}

onMounted(fetchContainers);

const filtered = computed(() => {
  let list = containers.value;

  if (filterSource.value !== 'all') {
    list = list.filter(c => (c.category || 'unmanaged') === filterSource.value);
  }
  if (filterState.value !== 'all') {
    list = list.filter(c => (c.state || '').toLowerCase() === filterState.value);
  }
  const q = search.value.toLowerCase().trim();
  if (q) {
    list = list.filter(c =>
      (c.name || '').toLowerCase().includes(q) ||
      (c.image || '').toLowerCase().includes(q) ||
      (c.id || '').toLowerCase().includes(q)
    );
  }
  return list;
});

const totalCount = computed(() => containers.value.length);
const stackCount = computed(() => containers.value.filter(c => c.category === 'managed').length);
const swarmCount = computed(() => containers.value.filter(c => c.category === 'swarm').length);
const unmanagedCount = computed(() => containers.value.filter(c => !c.category || c.category === 'unmanaged').length);

function stateDot(state) {
  if (state === 'running') return 'bg-green-500';
  if (state === 'exited') return 'bg-red-500';
  if (state === 'paused') return 'bg-yellow-500';
  if (state === 'restarting') return 'bg-blue-500';
  return 'bg-gray-400';
}

function managedBadge(source) {
  if (source === 'managed') return { label: 'Stack', classes: 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300' };
  if (source === 'swarm') return { label: 'Swarm', classes: 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300' };
  return { label: 'Unmanaged', classes: 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300' };
}

function truncateId(id) {
  return id ? id.substring(0, 12) : '-';
}
</script>

<template>
  <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
    <div class="p-4 border-b border-gray-200 dark:border-gray-700">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Containers</h3>
        <button
          @click="fetchContainers"
          :disabled="!isConnected"
          class="px-2.5 py-1 rounded text-xs font-medium bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-600 disabled:opacity-40 transition-colors"
        >Refresh</button>
      </div>

      <div class="grid grid-cols-4 gap-3 mb-3">
        <div class="rounded bg-gray-50 dark:bg-gray-900/30 p-2 text-center">
          <div class="text-lg font-bold text-gray-900 dark:text-gray-100">{{ totalCount }}</div>
          <div class="text-[10px] text-gray-500 dark:text-gray-400 uppercase tracking-wide">Total</div>
        </div>
        <div class="rounded bg-gray-50 dark:bg-gray-900/30 p-2 text-center">
          <div class="text-lg font-bold text-blue-600 dark:text-blue-400">{{ stackCount }}</div>
          <div class="text-[10px] text-gray-500 dark:text-gray-400 uppercase tracking-wide">Stack Managed</div>
        </div>
        <div class="rounded bg-gray-50 dark:bg-gray-900/30 p-2 text-center">
          <div class="text-lg font-bold text-purple-600 dark:text-purple-400">{{ swarmCount }}</div>
          <div class="text-[10px] text-gray-500 dark:text-gray-400 uppercase tracking-wide">Swarm</div>
        </div>
        <div class="rounded bg-gray-50 dark:bg-gray-900/30 p-2 text-center">
          <div class="text-lg font-bold text-gray-600 dark:text-gray-300">{{ unmanagedCount }}</div>
          <div class="text-[10px] text-gray-500 dark:text-gray-400 uppercase tracking-wide">Unmanaged</div>
        </div>
      </div>

      <div class="flex items-center gap-2 flex-wrap">
        <input
          v-model="search"
          type="text"
          placeholder="Search containers..."
          class="px-2 py-1.5 text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500 flex-1 min-w-[140px]"
        />
        <select
          v-model="filterSource"
          class="px-2 py-1.5 text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500"
        >
          <option value="all">All sources</option>
          <option value="managed">Stack managed</option>
          <option value="swarm">Swarm</option>
          <option value="unmanaged">Unmanaged</option>
        </select>
        <select
          v-model="filterState"
          class="px-2 py-1.5 text-xs rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-1 focus:ring-blue-500"
        >
          <option value="all">All states</option>
          <option value="running">Running</option>
          <option value="exited">Exited</option>
        </select>
      </div>
    </div>

    <div v-if="loading" class="p-4 text-center text-sm text-gray-500 dark:text-gray-400">Loading containers...</div>

    <div v-else-if="!filtered.length" class="p-4 text-center text-sm text-gray-400 dark:text-gray-500">
      {{ containers.length ? 'No containers match filters.' : 'No containers found.' }}
    </div>

    <div v-else class="divide-y divide-gray-100 dark:divide-gray-700/50">
      <div
        v-for="c in filtered"
        :key="c.id"
        class="px-4 py-3 flex items-center gap-3 hover:bg-gray-50 dark:hover:bg-gray-750"
      >
        <span class="w-2 h-2 rounded-full flex-shrink-0" :class="stateDot(c.state)"></span>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{{ c.name }}</span>
            <span
              class="px-1.5 py-0.5 rounded text-[10px] font-medium flex-shrink-0"
              :class="managedBadge(c.category).classes"
            >{{ managedBadge(c.category).label }}</span>
            <span
              class="px-1.5 py-0.5 rounded text-[10px] font-medium flex-shrink-0"
              :class="c.state === 'running' ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' : 'bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300'"
            >{{ c.state }}</span>
          </div>
          <div class="flex items-center gap-3 mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            <span class="font-mono">{{ truncateId(c.id) }}</span>
            <span class="truncate max-w-[200px]">{{ c.image }}</span>
            <span v-if="c.stackName" class="text-blue-500 dark:text-blue-400">{{ c.stackName }}</span>
            <span v-if="c.service" class="text-purple-500 dark:text-purple-400">{{ c.service }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
