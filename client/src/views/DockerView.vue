<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { clientIds, dockerStatusMap, clientHasCapability } from '../lib/sharedWS.js';
import DockerOverview from '../components/DockerOverview.vue';
import DockerContainerList from '../components/DockerContainerList.vue';
import DockerServiceList from '../components/DockerServiceList.vue';
import DockerNetworkList from '../components/DockerNetworkList.vue';
import DockerNodeList from '../components/DockerNodeList.vue';

const route = useRoute();
const clientId = computed(() => String(route.params.clientId || ''));

const isConnected = computed(() => {
  const entry = clientIds.value.find(c => c.clientId === clientId.value);
  return entry?.connected !== false;
});

const dockerStatus = computed(() => dockerStatusMap[clientId.value] || null);
const isSwarmManager = computed(() => {
  const d = dockerStatus.value;
  return d?.swarm?.localNodeState === 'active' && d?.swarm?.controlAvailable;
});
// Additive migration to the generic capability registry (see DockerOverview.vue).
const hasDocker = computed(() => clientHasCapability(clientId.value, 'docker') || !!dockerStatus.value?.available);
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8 space-y-4">
    <div v-if="!isConnected" class="rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 px-4 py-3">
      <span class="text-sm text-amber-800 dark:text-amber-300 font-medium">Node offline &mdash; Docker data may be stale and actions are disabled.</span>
    </div>

    <DockerOverview :client-id="clientId" />

    <DockerContainerList :client-id="clientId" :is-connected="isConnected" />

    <template v-if="hasDocker">
      <DockerServiceList v-if="isSwarmManager" :client-id="clientId" :is-connected="isConnected" />
      <DockerNodeList v-if="isSwarmManager" :client-id="clientId" :is-connected="isConnected" />
      <DockerNetworkList :client-id="clientId" :is-connected="isConnected" />
    </template>

    <div v-if="!hasDocker && isConnected" class="rounded-lg bg-white dark:bg-gray-800 p-8 text-center">
      <svg class="w-12 h-12 mx-auto text-gray-300 dark:text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4" /></svg>
      <div class="text-sm text-gray-500 dark:text-gray-400">No Docker data available for this node.</div>
    </div>
  </main>
</template>
