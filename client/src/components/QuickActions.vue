<template>
  <div class=" py-2 shadow-sm border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
    <div v-if="selectedClient && !isClientConnected" class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 pb-1">
      <span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
        Node offline — management actions disabled
      </span>
    </div>
    <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 flex items-center space-x-3 ">
        <button
      class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed"
          :disabled="busyRestart || !isClientConnected"
          title="Restart Server"
          @click="restartServer"
        >
          <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1 2.13-9"/>
          </svg>
        </button>
        <button
      class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed"
          :disabled="busyShutdown || !isClientConnected"
          title="Shutdown Server"
          @click="shutdownServer"
        >
          <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 2v10"/><path d="M5.5 5.5a7.5 7.5 0 1 0 13 0"/>
          </svg>
        </button>
        <button
      class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed"
          :disabled="!canOpenShell"
          title="Open Shell"
          @click="openShell"
        >
          <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 17l6-6-6-6"/><path d="M12 19h8"/>
          </svg>
        </button>
        <button
      class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed"
          :disabled="!canCloseShell"
          title="Close Shell"
          @click="closeShell"
        >
          <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>
        <div class="flex items-center space-x-1" v-if="selectedClient">
          <button
            class="p-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed"
            :disabled="!selectedClient || syncingKeys || !githubUser || !isClientConnected"
            :title="!githubUser ? 'Set VITE_GITHUB_USERNAME in your environment to enable this action.' : (syncingKeys ? 'Syncing...' : 'Re-sync GitHub SSH Keys')"
            @click="resyncKeys"
          >
            <svg class="w-5 h-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 2v6h-6"/><path d="M3 12a9 9 0 0 1 15-6.7L21 8"/><path d="M3 22v-6h6"/><path d="M21 12a9 9 0 0 1-15 6.7L3 16"/>
            </svg>
          </button>
        </div>

        <!-- Kiosk status + link -->
        <div v-if="selectedClient && kioskStatus" class="border-l border-gray-300 dark:border-gray-600 h-6 mx-2"></div>
        <router-link
          v-if="selectedClient"
          :to="`/client/${encodeURIComponent(selectedClient)}/kiosk`"
          class="flex items-center space-x-1.5 px-2 py-1 rounded text-xs hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          title="Kiosk settings"
        >
          <span
            v-if="kioskStatus"
            class="w-2 h-2 rounded-full flex-shrink-0"
            :class="kioskStatus.connected ? 'bg-green-500' : (kioskStatus.running ? 'bg-yellow-500' : 'bg-gray-400')"
          ></span>
          <span class="text-gray-600 dark:text-gray-300 font-medium">Kiosk</span>
          <span v-if="kioskStatus?.content" class="text-gray-400 dark:text-gray-500">{{ kioskContentLabel }}</span>
        </router-link>
        <button
          v-if="selectedClient && !isKioskActive"
          :disabled="switchingVariant"
          class="px-2 py-1 text-xs rounded bg-purple-600 text-white hover:bg-purple-700 disabled:opacity-40 transition-colors"
          title="Enable kiosk mode on this agent"
          @click="switchToKiosk"
        >{{ switchingVariant ? 'Switching...' : 'Enable Kiosk' }}</button>
        <button
          v-if="selectedClient && isKioskActive"
          :disabled="switchingVariant"
          class="px-2 py-1 text-xs rounded bg-gray-500 text-white hover:bg-gray-600 disabled:opacity-40 transition-colors"
          title="Disable kiosk mode on this agent"
          @click="switchToHeadless"
        >{{ switchingVariant ? 'Switching...' : 'Disable Kiosk' }}</button>
    </div>
    <div class="flex-1"></div>
    <div v-if="selectedClient" class="max-w-7xl mx-auto px-12  text-xs px-4text-gray-500 dark:text-gray-400">Client: <span class="font-mono">{{ selectedClient }}</span></div>
  </div>
</template>
<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { send, on as onWS, kioskStatusMap, variantStatusMap, clientIds } from '../lib/sharedWS.js';

const props = defineProps({
  selectedClient: { type: String, default: '' }
});

const currentSession = ref('');
const githubUser = import.meta.env.VITE_GITHUB_USERNAME || '';
const syncingKeys = ref(false);
const busyRestart = ref(false);
const busyShutdown = ref(false);

const isClientConnected = computed(() => {
  if (!props.selectedClient) return false;
  const entry = clientIds.value.find(c => c.clientId === props.selectedClient);
  return entry?.connected !== false;
});

const switchingVariant = ref(false);

const kioskStatus = computed(() => {
  if (!props.selectedClient) return null;
  return kioskStatusMap[props.selectedClient] || null;
});

const variantStatus = computed(() => {
  if (!props.selectedClient) return null;
  return variantStatusMap[props.selectedClient] || null;
});

const kioskContentLabel = computed(() => {
  const kind = kioskStatus.value?.content?.kind || 'blank';
  const labels = { blank: 'Blank', dashboard: 'Dashboard', message: 'Message', url: 'URL', page: 'Page' };
  return labels[kind] || kind;
});

const isKioskActive = computed(() => {
  const vs = variantStatus.value;
  if (vs) return vs.current === 'kiosk';
  const ks = kioskStatus.value;
  return ks?.connected || ks?.running || false;
});

function switchToKiosk() {
  if (!props.selectedClient || switchingVariant.value) return;
  switchingVariant.value = true;
  send({ type: 'switch_variant', clientId: props.selectedClient, variant: 'kiosk' });
}
function switchToHeadless() {
  if (!props.selectedClient || switchingVariant.value) return;
  switchingVariant.value = true;
  send({ type: 'switch_variant', clientId: props.selectedClient, variant: 'headless' });
}

function restartServer(){
  if (!props.selectedClient) return;
  busyRestart.value = true;
  fetch('/api/server/restart', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ clientId: props.selectedClient })
  }).finally(()=> busyRestart.value=false);
}
function shutdownServer(){
  if (!props.selectedClient) return;
  busyShutdown.value = true;
  fetch('/api/server/shutdown', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ clientId: props.selectedClient })
  }).finally(()=> busyShutdown.value=false);
}

function openShell(){
  if (!props.selectedClient) return;
  send({ type: 'shell_start', clientId: props.selectedClient });
}
function closeShell(){
  if (!currentSession.value) return;
  send({ type: 'shell_close', session: currentSession.value });
}
async function resyncKeys(){
  if (!props.selectedClient) return;
  syncingKeys.value = true;
  try {
    if (!githubUser) return;
    const body = { githubUser };
    await fetch(`/api/client/${encodeURIComponent(props.selectedClient)}/keys/resync`, {
      method:'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
  } catch {}
  finally { syncingKeys.value=false; }
}

const canOpenShell = computed(()=> !!props.selectedClient && !currentSession.value && isClientConnected.value);
const canCloseShell = computed(()=> !!currentSession.value);

const off = [];
function handleStarted(msg){ if (msg.clientId === props.selectedClient) currentSession.value = msg.session; }
function handleClosed(msg){ if (msg.session === currentSession.value) currentSession.value=''; }

onMounted(()=> {
  off.push(onWS('shell_started', handleStarted));
  off.push(onWS('shell_closed', handleClosed));
  off.push(onWS('variant_switch_result', (msg) => {
    if (msg.clientId === props.selectedClient) switchingVariant.value = false;
  }));
});
onUnmounted(()=> off.forEach(fn=>fn()));
</script>
