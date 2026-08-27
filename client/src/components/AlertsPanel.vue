<template>
  <section
    v-if="hasAlerts || hasCritical"
    class="bg-white dark:bg-gray-800 shadow rounded-lg border overflow-hidden"
    :class="hasCritical ? 'border-red-300 dark:border-red-700' : 'border-orange-200 dark:border-orange-800'"
  >
    <header 
      class="px-3 py-2 border-b flex items-center justify-between cursor-pointer"
      :class="hasCritical 
        ? 'border-red-200 dark:border-red-700 bg-red-50 dark:bg-red-900/30' 
        : 'border-orange-200 dark:border-orange-700 bg-orange-50 dark:bg-orange-900/20'"
      @click="expanded = !expanded"
    >
      <div class="flex items-center gap-2">
        <svg 
          class="w-5 h-5" 
          :class="hasCritical ? 'text-red-600 dark:text-red-400' : 'text-orange-600 dark:text-orange-400'"
          fill="none" 
          stroke="currentColor" 
          viewBox="0 0 24 24"
        >
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
        <h3 
          class="text-sm font-semibold"
          :class="hasCritical ? 'text-red-800 dark:text-red-200' : 'text-orange-800 dark:text-orange-200'"
        >
          OS Alerts
        </h3>
        <span 
          v-if="hasCritical"
          class="text-xs px-1.5 py-0.5 rounded-full bg-red-100 dark:bg-red-800 text-red-700 dark:text-red-200 font-medium"
        >
          Critical
        </span>
      </div>
      <div class="flex items-center gap-2">
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ totalCount }} alert(s)</span>
        <svg 
          class="w-4 h-4 text-gray-400 transform transition-transform"
          :class="{ 'rotate-180': expanded }"
          fill="none" 
          stroke="currentColor" 
          viewBox="0 0 24 24"
        >
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
        </svg>
      </div>
    </header>
    
    <div v-if="expanded" class="px-3 py-2 max-h-64 overflow-auto text-xs space-y-2">
      <div v-if="lastScanTime" class="text-gray-500 dark:text-gray-400 mb-2">
        Last scanned: <span class="font-mono">{{ formatTime(lastScanTime) }}</span>
      </div>
      
      <div 
        v-for="alert in alerts" 
        :key="alert.id" 
        class="p-2 rounded border"
        :class="alertClasses(alert.severity)"
      >
        <div class="flex items-start justify-between gap-2">
          <div class="flex items-center gap-2">
            <span 
              class="text-xs px-1.5 py-0.5 rounded font-medium uppercase"
              :class="severityBadgeClasses(alert.severity)"
            >
              {{ alert.severity }}
            </span>
            <span 
              class="text-xs px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300"
            >
              {{ formatCategory(alert.category) }}
            </span>
          </div>
          <span class="text-gray-400 dark:text-gray-500 font-mono whitespace-nowrap">
            {{ formatTime(alert.ts) }}
          </span>
        </div>
        <p 
          class="mt-1 font-mono text-gray-700 dark:text-gray-300 break-words"
          :class="{ 'line-clamp-2': !expandedAlerts[alert.id] }"
          @click="toggleAlertExpand(alert.id)"
        >
          {{ alert.message }}
        </p>
        <div v-if="alert.count > 1" class="mt-1 text-gray-500 dark:text-gray-400">
          ({{ alert.count }} occurrences)
        </div>
        <div class="mt-1 text-gray-400 dark:text-gray-500">
          Source: {{ alert.source }}
        </div>
      </div>
      
      <div v-if="alerts.length === 0" class="text-gray-500 dark:text-gray-400 text-center py-4">
        No alerts detected in the monitoring window.
      </div>
    </div>
  </section>
</template>

<script setup>
import { ref, computed, reactive } from 'vue';
import { alertsMap, ensureClientAlertsLoaded } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, required: true }
});

const expanded = ref(true);
const expandedAlerts = reactive({});

// Load alerts on mount if not already present
ensureClientAlertsLoaded(props.clientId);

const alertData = computed(() => alertsMap[props.clientId] || null);
const alerts = computed(() => alertData.value?.alerts || []);
const totalCount = computed(() => alertData.value?.totalCount || 0);
const hasCritical = computed(() => alertData.value?.hasCritical || false);
const hasAlerts = computed(() => alerts.value.length > 0);
const lastScanTime = computed(() => alertData.value?.lastScanTime || '');

function formatTime(ts) {
  if (!ts) return '-';
  try {
    return new Date(ts).toLocaleString();
  } catch {
    return ts;
  }
}

function formatCategory(cat) {
  if (!cat) return 'unknown';
  return cat.replace(/_/g, ' ');
}

function alertClasses(severity) {
  switch (severity) {
    case 'critical':
      return 'border-red-200 dark:border-red-700 bg-red-50 dark:bg-red-900/20';
    case 'error':
      return 'border-orange-200 dark:border-orange-700 bg-orange-50 dark:bg-orange-900/20';
    case 'warning':
      return 'border-yellow-200 dark:border-yellow-700 bg-yellow-50 dark:bg-yellow-900/20';
    default:
      return 'border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/20';
  }
}

function severityBadgeClasses(severity) {
  switch (severity) {
    case 'critical':
      return 'bg-red-100 dark:bg-red-800 text-red-700 dark:text-red-200';
    case 'error':
      return 'bg-orange-100 dark:bg-orange-800 text-orange-700 dark:text-orange-200';
    case 'warning':
      return 'bg-yellow-100 dark:bg-yellow-800 text-yellow-700 dark:text-yellow-200';
    default:
      return 'bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300';
  }
}

function toggleAlertExpand(id) {
  expandedAlerts[id] = !expandedAlerts[id];
}
</script>
