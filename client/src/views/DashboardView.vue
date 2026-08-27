<script setup>
import { computed, ref } from 'vue';
import { useRoute } from 'vue-router';

import Dashboard from '../components/Dashboard.vue';
import CronEditor from '../components/CronEditor.vue';
import AgentUpdateTerminal from '../components/AgentUpdateTerminal.vue';

const route = useRoute();
const clientId = computed(() => String(route.params.clientId || ''));
const showCron = ref(false);
const showUpdateLog = ref(false);
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8 space-y-4">
    <Dashboard :selected-client="clientId" />

    <!-- Tools -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
        <button class="flex items-center justify-between w-full text-left p-5" @click="showCron = !showCron">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Cron Editor</h3>
          <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showCron }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
        <div v-if="showCron" class="px-5 pb-5">
          <CronEditor :scope-client="clientId" />
        </div>
      </div>

      <div class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
        <button class="flex items-center justify-between w-full text-left p-5" @click="showUpdateLog = !showUpdateLog">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Agent Update Log</h3>
          <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': showUpdateLog }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
        <div v-if="showUpdateLog" class="px-5 pb-5">
          <AgentUpdateTerminal :client-id="clientId" />
        </div>
      </div>
    </div>
  </main>
</template>
