<script setup>
import { ref, onMounted } from 'vue';
import { fetchEnvGroups } from '../lib/sharedWS.js';

const groups = ref([]);
const loading = ref(true);
const expandedId = ref(null);
const showCreate = ref(false);
const revealSecrets = ref({});

const newGroup = ref({ name: '', description: '', scope: 'global' });
const newVar = ref({ key: '', value: '', sensitive: false });

async function loadGroups() {
  loading.value = true;
  groups.value = await fetchEnvGroups();
  loading.value = false;
}

onMounted(loadGroups);

function toggleExpand(id) {
  expandedId.value = expandedId.value === id ? null : id;
}

function toggleReveal(groupId) {
  revealSecrets.value[groupId] = !revealSecrets.value[groupId];
}

async function createGroup() {
  if (!newGroup.value.name.trim()) return;
  try {
    await fetch('/api/env-groups', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(newGroup.value),
    });
    newGroup.value = { name: '', description: '', scope: 'global' };
    showCreate.value = false;
    await loadGroups();
  } catch {}
}

async function deleteGroup(id) {
  try {
    await fetch(`/api/env-groups/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      credentials: 'include',
    });
    await loadGroups();
  } catch {}
}

async function addVariable(group) {
  if (!newVar.value.key.trim()) return;
  const vars = [...(group.variables || []), { ...newVar.value }];
  try {
    await fetch(`/api/env-groups/${encodeURIComponent(group.id)}`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...group, variables: vars }),
    });
    newVar.value = { key: '', value: '', sensitive: false };
    await loadGroups();
  } catch {}
}

async function removeVariable(group, idx) {
  const vars = [...(group.variables || [])];
  vars.splice(idx, 1);
  try {
    await fetch(`/api/env-groups/${encodeURIComponent(group.id)}`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...group, variables: vars }),
    });
    await loadGroups();
  } catch {}
}

async function saveGroup(group) {
  try {
    await fetch(`/api/env-groups/${encodeURIComponent(group.id)}`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(group),
    });
    await loadGroups();
  } catch {}
}
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Environment Groups</h1>
      <button
        @click="showCreate = !showCreate"
        class="px-3 py-1.5 rounded text-xs font-medium bg-blue-600 text-white hover:bg-blue-700 transition-colors"
      >{{ showCreate ? 'Cancel' : 'New Group' }}</button>
    </div>

    <!-- Create form -->
    <div v-if="showCreate" class="rounded-lg bg-white dark:bg-gray-800 p-4 mb-4">
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
        <input
          v-model="newGroup.name"
          type="text"
          placeholder="Group name"
          class="px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <input
          v-model="newGroup.description"
          type="text"
          placeholder="Description (optional)"
          class="px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <select
          v-model="newGroup.scope"
          class="px-3 py-2 text-sm rounded-lg border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="global">Global</option>
          <option value="stack">Stack</option>
        </select>
      </div>
      <button
        @click="createGroup"
        :disabled="!newGroup.name.trim()"
        class="px-3 py-1.5 rounded text-xs font-medium bg-green-600 text-white hover:bg-green-700 disabled:opacity-40 transition-colors"
      >Create Group</button>
    </div>

    <div v-if="loading" class="text-center py-12 text-gray-500 dark:text-gray-400 text-sm">Loading groups...</div>

    <div v-else-if="!groups.length" class="text-center py-12 text-gray-400 dark:text-gray-500 text-sm">No environment groups yet.</div>

    <div v-else class="space-y-3">
      <div v-for="group in groups" :key="group.id" class="rounded-lg bg-white dark:bg-gray-800 overflow-hidden">
        <button
          @click="toggleExpand(group.id)"
          class="w-full flex items-center justify-between p-4 text-left hover:bg-gray-50 dark:hover:bg-gray-750 transition-colors"
        >
          <div>
            <div class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ group.name }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ group.description || 'No description' }}
              <span class="ml-2 px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 text-[10px] font-medium">{{ group.scope || 'global' }}</span>
              <span class="ml-1">{{ (group.variables || []).length }} var{{ (group.variables || []).length !== 1 ? 's' : '' }}</span>
            </div>
          </div>
          <svg class="w-4 h-4 text-gray-400 transition-transform" :class="{ 'rotate-180': expandedId === group.id }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
          </svg>
        </button>

        <div v-if="expandedId === group.id" class="border-t border-gray-200 dark:border-gray-700 p-4">
          <div class="flex items-center justify-between mb-3">
            <button
              @click="toggleReveal(group.id)"
              class="text-xs text-blue-600 dark:text-blue-400 hover:underline"
            >{{ revealSecrets[group.id] ? 'Hide sensitive' : 'Reveal sensitive' }}</button>
            <button
              @click="deleteGroup(group.id)"
              class="text-xs text-red-600 dark:text-red-400 hover:underline"
            >Delete group</button>
          </div>

          <div v-if="(group.variables || []).length" class="space-y-1 mb-3">
            <div
              v-for="(v, idx) in group.variables"
              :key="idx"
              class="flex items-center gap-2 text-xs"
            >
              <span class="font-mono text-gray-900 dark:text-gray-100 min-w-[120px]">{{ v.key }}</span>
              <span class="font-mono text-gray-500 dark:text-gray-400 flex-1 truncate">
                {{ v.sensitive && !revealSecrets[group.id] ? '••••••' : v.value }}
              </span>
              <span v-if="v.sensitive" class="px-1 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 text-[10px]">secret</span>
              <button @click="removeVariable(group, idx)" class="text-red-500 hover:text-red-700 dark:hover:text-red-400 flex-shrink-0" title="Remove">
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg>
              </button>
            </div>
          </div>

          <div class="flex items-center gap-2">
            <input
              v-model="newVar.key"
              type="text"
              placeholder="KEY"
              class="px-2 py-1.5 text-xs font-mono rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500 w-32"
            />
            <input
              v-model="newVar.value"
              type="text"
              placeholder="value"
              class="px-2 py-1.5 text-xs font-mono rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 focus:outline-none focus:ring-1 focus:ring-blue-500 flex-1"
            />
            <label class="flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400 cursor-pointer">
              <input type="checkbox" v-model="newVar.sensitive" class="rounded border-gray-300 dark:border-gray-600" />
              Secret
            </label>
            <button
              @click="addVariable(group)"
              :disabled="!newVar.key.trim()"
              class="px-2 py-1.5 rounded text-xs font-medium bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-40 transition-colors"
            >Add</button>
          </div>
        </div>
      </div>
    </div>
  </main>
</template>
