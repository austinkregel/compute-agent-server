<script setup>
import { ref, watch, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import ClientDropdown from './components/ClientDropdown.vue';
import ClientSwitchPalette from './components/ClientSwitchPalette.vue';
import { clientIds, connected, ensureClientStatsLoaded, clientHasCapability } from './lib/sharedWS.js';
import { isAuthenticated, isAdmin, user, logout, checkAuth } from './lib/auth.js';

const route = useRoute();
const router = useRouter();
const selectedClient = ref(String(route.params.clientId || ''));

// Disconnected nodes remain in clientIds with connected: false,
// so the selection stays valid even when the node goes offline.

const statusLabel = computed(()=> connected.value ? 'Online' : 'Offline');
const statusClasses = computed(()=> connected.value ? 'bg-green-100 text-green-800' : 'bg-red-100 text-red-800');
const dotClasses = computed(()=> connected.value ? 'bg-green-400' : 'bg-red-400');
const dark = ref(false);
function toggleDark(){ dark.value = !dark.value; persistPref(); applyDark(); }
function applyDark(){
  const cls = document.documentElement.classList;
  if (dark.value) cls.add('dark'); else cls.remove('dark');
}
function persistPref(){ try { localStorage.setItem('ui.dark', dark.value ? '1':'0'); } catch {}}
onMounted(async ()=> { 
  try { dark.value = localStorage.getItem('ui.dark')==='1' || ( !localStorage.getItem('ui.dark') && window.matchMedia('(prefers-color-scheme: dark)').matches); } catch {} 
  applyDark();
  // Check auth status on mount
  await checkAuth();
});

function handleLogout() {
  logout();
}

// Sync selection from the route (supports back/forward and deep links).
watch(() => route.params.clientId, (cid) => {
  selectedClient.value = String(cid || '');
}, { immediate: true });

// When selection changes (dropdown), navigate to that client's current page (default to dashboard).
watch(selectedClient, async (cid, prev) => {
  if (cid && cid !== prev) {
    // Preserve subpage if we're already on one; otherwise go to dashboard.
    const base = `/client/${encodeURIComponent(cid)}`;
    const onClientRoute = String(route.path || '').startsWith('/client/');
    const target = onClientRoute ? route.path.replace(/\/client\/[^/]+/, base) : base;
    if (target !== route.path) await router.push(target);
    await ensureClientStatsLoaded(cid);
  }
  if (!cid && prev) {
    await router.push('/');
  }
}, { immediate: false });

const showClientNav = computed(() => !!selectedClient.value);
const clientBase = computed(() => selectedClient.value ? `/client/${encodeURIComponent(selectedClient.value)}` : '');
// SMS is only meaningful for phone-class agents (telephony capability) and is
// admin-gated server-side (see router.js) — hide the tab entirely rather
// than showing a dead end for everyone else.
const showSmsTab = computed(() => isAdmin.value && clientHasCapability(selectedClient.value, 'telephony'));
function tabClass(active) {
  return [
    'px-3 py-1.5 rounded text-xs font-medium border transition-colors',
    active
      ? 'bg-gray-900 text-white border-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:border-gray-100'
      : 'bg-transparent text-gray-600 border-transparent hover:text-gray-900 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-100 dark:hover:bg-gray-700',
  ].join(' ');
}
</script>

<template>
  <div :class="['min-h-screen max-w-screen overflow-y-hidden', dark ? 'bg-gray-900 text-gray-100' : 'bg-gray-50 text-gray-900']">
    <!-- Header -->
  <header class="shadow-sm border-b border-gray-200" :class="dark ? 'bg-gray-800 border-gray-700' : 'bg-white'">
      <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div class="flex justify-between items-center h-16">
          <div class="flex items-center space-x-4">
            <router-link to="/" class="text-xl font-semibold hover:opacity-80 transition-opacity" :class="dark ? 'text-gray-100':'text-gray-900'">Backup Server</router-link>
            <router-link
              to="/fleet"
              class="px-2.5 py-1 rounded text-xs font-medium border transition-colors"
              :class="route.path === '/fleet'
                ? 'bg-gray-900 text-white border-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:border-gray-100'
                : 'bg-transparent text-gray-600 border-transparent hover:text-gray-900 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-100 dark:hover:bg-gray-700'"
            >Fleet</router-link>
            <!-- Stacks / Env nav removed: the agent is monitoring-only and stack
                 deployment/management is owned by a separate tool. The views and
                 routes are retained but no longer advertised in the nav. -->
            <router-link
              v-if="isAdmin"
              to="/admin/exec-allowlist"
              class="px-2.5 py-1 rounded text-xs font-medium border transition-colors"
              :class="route.path === '/admin/exec-allowlist'
                ? 'bg-gray-900 text-white border-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:border-gray-100'
                : 'bg-transparent text-gray-600 border-transparent hover:text-gray-900 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-100 dark:hover:bg-gray-700'"
            >Allowlist</router-link>
            <ClientDropdown v-model="selectedClient" :clients="clientIds" />
          </div>
          <div class="flex items-center space-x-4" id="header-meta">
            <button type="button" @click="toggleDark" class="px-2 py-1 rounded text-xs font-medium focus:outline-none transition-colors"
              :class="dark ? 'bg-gray-700 text-gray-200 hover:bg-gray-600' : 'bg-gray-200 text-gray-700 hover:bg-gray-300'">
              {{ dark ? 'Light Mode' : 'Dark Mode' }}
            </button>
            <span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium" :class="statusClasses">
              <span class="w-2 h-2 rounded-full mr-1.5" :class="dotClasses"></span>
              {{ statusLabel }}
            </span>
            <div v-if="isAuthenticated && user.email" class="flex items-center space-x-2">
              <span class="text-sm" :class="dark ? 'text-gray-300' : 'text-gray-700'">
                {{ user.name || user.email }}
              </span>
              <button 
                @click="handleLogout"
                class="px-2 py-1 rounded text-xs font-medium focus:outline-none"
                :class="dark ? 'bg-gray-700 text-gray-200 hover:bg-gray-600' : 'bg-gray-200 text-gray-700 hover:bg-gray-300'"
              >
                Logout
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="showClientNav" class="border-t border-gray-200 dark:border-gray-700">
        <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center gap-2">
          <router-link :to="clientBase" :class="tabClass(route.path === clientBase)">
            Dashboard
          </router-link>
          <router-link :to="`${clientBase}/backups`" :class="tabClass(route.path.endsWith('/backups'))">
            Backups
          </router-link>
          <router-link v-if="showSmsTab" :to="`${clientBase}/sms`" :class="tabClass(route.path.endsWith('/sms'))">
            SMS
          </router-link>
          <router-link :to="`${clientBase}/actions`" :class="tabClass(route.path.endsWith('/actions'))">
            Actions
          </router-link>
          <router-link :to="`${clientBase}/docker`" :class="tabClass(route.path.endsWith('/docker'))">
            Docker
          </router-link>
          <router-link :to="`${clientBase}/kiosk`" :class="tabClass(route.path.endsWith('/kiosk'))">
            Kiosk
          </router-link>
          <router-link :to="`${clientBase}/logs`" :class="tabClass(route.path.endsWith('/logs'))">
            Logs
          </router-link>
          <div class="ml-auto text-xs text-gray-500 dark:text-gray-400">
            Client: <span class="font-mono">{{ selectedClient }}</span>
          </div>
        </div>
      </div>
    </header>

    <!-- Main Content -->
    <div class="overflow-y-auto overflow-x-hidden max-w-screen w-full h-full" style="max-height: calc(100vh - 64px);">
      <router-view />
    </div>

    <!-- Client Switch Command Palette (Ctrl+K / Cmd+K) -->
    <ClientSwitchPalette />
  </div>
</template>
