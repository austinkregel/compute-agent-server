<template>
  <TransitionRoot :show="open" as="template" @after-leave="query = ''">
    <Dialog class="relative z-50" @close="close">
      <TransitionChild
        as="template"
        enter="ease-out duration-200"
        enter-from="opacity-0"
        enter-to="opacity-100"
        leave="ease-in duration-150"
        leave-from="opacity-100"
        leave-to="opacity-0"
      >
        <div class="fixed inset-0 bg-gray-500/50 dark:bg-gray-900/70 transition-opacity" />
      </TransitionChild>

      <div class="fixed inset-0 z-50 overflow-y-auto p-4 sm:p-6 md:p-20">
        <TransitionChild
          as="template"
          enter="ease-out duration-200"
          enter-from="opacity-0 scale-95"
          enter-to="opacity-100 scale-100"
          leave="ease-in duration-150"
          leave-from="opacity-100 scale-100"
          leave-to="opacity-0 scale-95"
        >
          <DialogPanel
            class="mx-auto max-w-xl transform divide-y divide-gray-100 dark:divide-gray-700 overflow-hidden rounded-xl bg-white dark:bg-gray-800 shadow-2xl ring-1 ring-black/5 dark:ring-white/10 transition-all"
          >
            <Combobox @update:modelValue="onSelect">
              <div class="relative">
                <svg
                  class="pointer-events-none absolute left-4 top-3.5 h-5 w-5 text-gray-400 dark:text-gray-500"
                  viewBox="0 0 20 20"
                  fill="currentColor"
                  aria-hidden="true"
                >
                  <path
                    fill-rule="evenodd"
                    d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z"
                    clip-rule="evenodd"
                  />
                </svg>
                <ComboboxInput
                  class="h-12 w-full border-0 bg-transparent pl-11 pr-4 text-gray-900 dark:text-gray-100 placeholder:text-gray-400 dark:placeholder:text-gray-500 focus:ring-0 sm:text-sm"
                  placeholder="Search clients..."
                  @change="query = $event.target.value"
                  autocomplete="off"
                />
              </div>

              <ComboboxOptions
                v-if="filteredClients.length > 0"
                static
                class="max-h-72 scroll-py-2 overflow-y-auto py-2 text-sm text-gray-800 dark:text-gray-200"
              >
                <ComboboxOption
                  v-for="client in filteredClients"
                  :key="client.clientId"
                  :value="client.clientId"
                  as="template"
                  v-slot="{ active }"
                >
                  <li
                    :class="[
                      'cursor-pointer select-none px-4 py-2 flex items-center justify-between',
                      active ? 'bg-indigo-600 text-white' : ''
                    ]"
                  >
                    <span class="font-medium truncate">{{ client.clientId }}</span>
                    <span
                      :class="[
                        'text-xs',
                        active ? 'text-indigo-200' : 'text-gray-500 dark:text-gray-400'
                      ]"
                    >
                      {{ isOnline(client) ? 'online' : 'offline' }}
                    </span>
                  </li>
                </ComboboxOption>
              </ComboboxOptions>

              <div
                v-if="query !== '' && filteredClients.length === 0"
                class="px-6 py-14 text-center text-sm sm:px-14"
              >
                <svg
                  class="mx-auto h-6 w-6 text-gray-400 dark:text-gray-500"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke-width="1.5"
                  stroke="currentColor"
                  aria-hidden="true"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M15.182 16.318A4.486 4.486 0 0012.016 15a4.486 4.486 0 00-3.198 1.318M21 12a9 9 0 11-18 0 9 9 0 0118 0zM9.75 9.75c0 .414-.168.75-.375.75S9 10.164 9 9.75 9.168 9 9.375 9s.375.336.375.75zm-.375 0h.008v.015h-.008V9.75zm5.625 0c0 .414-.168.75-.375.75s-.375-.336-.375-.75.168-.75.375-.75.375.336.375.75zm-.375 0h.008v.015h-.008V9.75z"
                  />
                </svg>
                <p class="mt-4 font-semibold text-gray-900 dark:text-gray-100">No clients found</p>
                <p class="mt-2 text-gray-500 dark:text-gray-400">
                  No clients match "{{ query }}".
                </p>
              </div>

              <div
                v-if="query === '' && filteredClients.length === 0"
                class="px-6 py-14 text-center text-sm sm:px-14"
              >
                <svg
                  class="mx-auto h-6 w-6 text-gray-400 dark:text-gray-500"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke-width="1.5"
                  stroke="currentColor"
                  aria-hidden="true"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 17.25v1.007a3 3 0 01-.879 2.122L7.5 21h9l-.621-.621A3 3 0 0115 18.257V17.25m6-12V15a2.25 2.25 0 01-2.25 2.25H5.25A2.25 2.25 0 013 15V5.25m18 0A2.25 2.25 0 0018.75 3H5.25A2.25 2.25 0 003 5.25m18 0V12a2.25 2.25 0 01-2.25 2.25H5.25A2.25 2.25 0 013 12V5.25"
                  />
                </svg>
                <p class="mt-4 font-semibold text-gray-900 dark:text-gray-100">No clients connected</p>
                <p class="mt-2 text-gray-500 dark:text-gray-400">
                  Connect an agent to get started.
                </p>
              </div>
            </Combobox>

            <div class="flex flex-wrap items-center bg-gray-50 dark:bg-gray-900/50 px-4 py-2.5 text-xs text-gray-700 dark:text-gray-400">
              Type to search
              <kbd class="mx-1 flex h-5 w-5 items-center justify-center rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 font-semibold text-gray-900 dark:text-gray-200 sm:mx-2">
                &uarr;
              </kbd>
              <kbd class="mx-1 flex h-5 w-5 items-center justify-center rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 font-semibold text-gray-900 dark:text-gray-200">
                &darr;
              </kbd>
              <span class="ml-1">to navigate</span>
              <kbd class="mx-2 flex h-5 items-center justify-center rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-1.5 font-semibold text-gray-900 dark:text-gray-200">
                Enter
              </kbd>
              <span>to select</span>
              <kbd class="ml-auto flex h-5 items-center justify-center rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 px-1.5 font-semibold text-gray-900 dark:text-gray-200">
                Esc
              </kbd>
              <span class="ml-2">to close</span>
            </div>
          </DialogPanel>
        </TransitionChild>
      </div>
    </Dialog>
  </TransitionRoot>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import {
  Dialog,
  DialogPanel,
  Combobox,
  ComboboxInput,
  ComboboxOptions,
  ComboboxOption,
  TransitionRoot,
  TransitionChild,
} from '@headlessui/vue';
import { clientIds, ensureClientStatsLoaded } from '../lib/sharedWS.js';
import { computeClientTargetPath, shouldOpenPalette } from '../lib/clientNav.js';

const ONLINE_THRESHOLD_MS = 300000; // 5 minutes

const router = useRouter();
const route = useRoute();

const open = ref(false);
const query = ref('');

// Sort clients alphabetically by clientId
const sortedClients = computed(() => {
  return [...clientIds.value].sort((a, b) => 
    String(a.clientId || '').localeCompare(String(b.clientId || ''))
  );
});

// Filter clients by query (substring match on clientId)
const filteredClients = computed(() => {
  const q = query.value.toLowerCase().trim();
  if (!q) {
    return sortedClients.value;
  }
  return sortedClients.value.filter(client => 
    String(client.clientId || '').toLowerCase().includes(q)
  );
});

// Check if a client is online based on lastPong
function isOnline(client) {
  const ts = client.lastPong;
  if (!ts) return false;
  const tsMs = typeof ts === 'number' ? ts : Date.parse(ts);
  if (isNaN(tsMs)) return false;
  return Date.now() - tsMs < ONLINE_THRESHOLD_MS;
}

// Handle client selection
async function onSelect(clientId) {
  if (!clientId) return;
  
  const targetPath = computeClientTargetPath({
    currentPath: route.path,
    nextClientId: clientId,
  });
  
  close();
  await router.push(targetPath);
  await ensureClientStatsLoaded(clientId);
}

function close() {
  open.value = false;
}

function openPalette() {
  open.value = true;
}

// Keyboard handler
function handleKeyDown(event) {
  if (shouldOpenPalette(event)) {
    event.preventDefault();
    openPalette();
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleKeyDown);
});

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeyDown);
});

// Expose for parent components if needed
defineExpose({ open: openPalette, close });
</script>
