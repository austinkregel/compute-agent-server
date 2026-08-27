<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
    <!-- Header -->
    <div>
      <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Exec Command Allowlist</h1>
      <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">
        Global policy pushed to <span class="font-medium">every connected agent</span>. Governs which
        commands agents may run (<code class="text-xs">exec</code> / <code class="text-xs">admin_run</code>).
      </p>
    </div>

    <!-- Allow-all warning -->
    <div v-if="empty" class="rounded-lg border border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-900/30 p-3">
      <p class="text-sm font-medium text-amber-800 dark:text-amber-300">⚠ The allowlist is empty — agents allow <strong>any</strong> command.</p>
      <p class="text-xs text-amber-700 dark:text-amber-400 mt-1">Add at least one entry to enforce a policy.</p>
    </div>

    <!-- Add entry -->
    <div class="rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 p-4 space-y-3">
      <label class="block text-sm font-semibold text-gray-900 dark:text-gray-100">Add a command</label>
      <div class="flex items-center gap-2">
        <input
          v-model="newCommand"
          @keyup.enter="addCommand"
          type="text"
          placeholder="e.g. git status  ·  curl -fSL https://github.com/austinkregel/rebase-indexer/..."
          class="flex-1 font-mono text-xs rounded-md border border-gray-300 dark:border-gray-600 focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500 dark:bg-gray-900 dark:text-gray-100 px-3 py-2"
        />
        <button
          @click="addCommand"
          type="button"
          :disabled="loading || !newCommand.trim()"
          class="px-3 py-2 text-xs rounded-md bg-green-600 hover:bg-green-700 text-white disabled:opacity-50 transition-colors"
        >Add</button>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        Matching is <span class="font-medium">token-prefix</span>: <code class="text-xs">curl</code> allows any
        <code class="text-xs">curl …</code>; scope it (e.g.
        <code class="text-xs">curl -fSL https://github.com/austinkregel/rebase-indexer/…</code>) to restrict it.
      </p>
    </div>

    <!-- Status message -->
    <div v-if="message" :class="['p-3 rounded-md text-sm',
      messageType==='success' ? 'bg-green-50 text-green-700 dark:bg-green-900/30 dark:text-green-300'
      : messageType==='error' ? 'bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300'
      : 'bg-gray-50 text-gray-600 dark:bg-gray-800 dark:text-gray-300']">
      {{ message }}
    </div>

    <!-- List -->
    <div class="rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden">
      <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          Current entries <span class="text-gray-500 dark:text-gray-400 font-normal">({{ entries.length }})</span>
        </h2>
        <button
          @click="load"
          type="button"
          :disabled="loading"
          class="px-3 py-1.5 text-xs rounded-md bg-gray-200 hover:bg-gray-300 text-gray-800 dark:bg-gray-700 dark:hover:bg-gray-600 dark:text-gray-100 transition-colors"
        >{{ loading ? 'Loading…' : 'Reload' }}</button>
      </div>

      <table v-if="entries.length" class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead>
          <tr class="text-left text-xs text-gray-500 dark:text-gray-400">
            <th class="px-4 py-2 font-medium">Command</th>
            <th class="px-4 py-2 font-medium">Source</th>
            <th class="px-4 py-2 font-medium text-right">Action</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-gray-700/50">
          <tr v-for="e in entries" :key="e.cmd" class="hover:bg-gray-50 dark:hover:bg-gray-700/40">
            <td class="px-4 py-2 font-mono text-xs text-gray-900 dark:text-gray-100 break-all">{{ e.cmd }}</td>
            <td class="px-4 py-2">
              <span :class="['inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium', sourceBadge(e.source)]">
                {{ e.source }}
              </span>
            </td>
            <td class="px-4 py-2 text-right">
              <button
                @click="removeCommand(e.cmd)"
                type="button"
                :disabled="loading"
                class="px-2.5 py-1 text-xs rounded-md text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/30 disabled:opacity-50 transition-colors"
              >Remove</button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else class="px-4 py-6 text-sm text-gray-500 dark:text-gray-400">No entries — allow-all is in effect.</div>
    </div>

    <!-- Policy explainer -->
    <details class="rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 p-4">
      <summary class="text-sm font-semibold text-gray-900 dark:text-gray-100 cursor-pointer">How the allowlist works</summary>
      <ul class="mt-3 space-y-2 text-xs text-gray-600 dark:text-gray-300 list-disc pl-5">
        <li><span class="font-medium">Prefix match:</span> an entry matches any command that starts with its tokens. <code>curl</code> allows every curl; a fully-scoped entry allows only that invocation (and longer).</li>
        <li><span class="font-medium">Empty list = allow-all:</span> if you remove every entry, agents accept any command. Clearing requires explicit confirmation.</li>
        <li><span class="font-medium">Forbidden characters:</span> entries containing <code>; | &amp; $ ` </code> or newlines are rejected — agents refuse such commands outright.</li>
        <li><span class="font-medium">Working directory</span> is confined to each agent's allowed roots; a blocked command returns exit code <code>126</code>.</li>
        <li><span class="font-medium">Provenance:</span> <code>config</code> = seeded from server config, <code>admin</code> = added here, <code>crucible</code> = auto-granted by the desktop indexer.</li>
        <li><span class="font-medium">Per-agent floor:</span> each agent also honors its local <code>admin.allowedCommands</code>, which this UI can't see or shrink — an agent may allow <em>more</em> than is listed here.</li>
      </ul>
    </details>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';

const entries = ref([]);
const empty = ref(false);
const newCommand = ref('');
const loading = ref(false);
const message = ref('');
const messageType = ref('info');

function setMessage(msg, type = 'info') {
  message.value = msg;
  messageType.value = type;
  setTimeout(() => { if (message.value === msg) message.value = ''; }, 6000);
}

function sourceBadge(source) {
  switch (source) {
    case 'crucible': return 'bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300';
    case 'admin': return 'bg-indigo-100 text-indigo-800 dark:bg-indigo-900/40 dark:text-indigo-300';
    default: return 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300'; // config
  }
}

function applyState(data) {
  entries.value = data.entries || [];
  empty.value = !!data.empty;
}

// agentsUpdated counts ONLY online agents; offline ones receive the policy when
// they reconnect. Surface both so the operator isn't surprised.
function pushFeedback(data, verb) {
  const n = data.agentsUpdated ?? 0;
  setMessage(`${verb}. Pushed to ${n} online agent${n === 1 ? '' : 's'}. Offline agents update on reconnect.`, 'success');
}

async function load() {
  loading.value = true;
  try {
    const res = await fetch('/api/server/exec-allowlist', {
      credentials: 'include',
      headers: { Accept: 'application/json' },
    });
    if (!res.ok) throw new Error(`status ${res.status}`);
    applyState(await res.json());
  } catch (e) {
    setMessage('Failed to load allowlist', 'error');
  } finally {
    loading.value = false;
  }
}

async function mutate(body) {
  return fetch('/api/server/exec-allowlist', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
    body: JSON.stringify(body),
  });
}

async function addCommand() {
  const cmd = newCommand.value.trim();
  if (!cmd) return;
  loading.value = true;
  try {
    const res = await mutate({ add: [cmd], source: 'admin' });
    const data = await res.json();
    if (!res.ok) {
      setMessage(data.error || 'Add failed', 'error');
      return;
    }
    applyState(data);
    newCommand.value = '';
    pushFeedback(data, `Added "${cmd}"`);
  } catch (e) {
    setMessage('Add failed', 'error');
  } finally {
    loading.value = false;
  }
}

async function removeCommand(cmd, confirmEmpty = false) {
  loading.value = true;
  try {
    const res = await mutate({ remove: [cmd], confirmEmpty });
    const data = await res.json();
    if (res.status === 400 && !confirmEmpty && /allow-all/i.test(data.error || '')) {
      // Removing this entry would empty the list (allow-all). Confirm first.
      loading.value = false;
      if (window.confirm('Removing this is the last entry — the allowlist will be EMPTY, which means agents allow ANY command. Continue?')) {
        return removeCommand(cmd, true);
      }
      return;
    }
    if (!res.ok) {
      setMessage(data.error || 'Remove failed', 'error');
      return;
    }
    applyState(data);
    pushFeedback(data, `Removed "${cmd}"`);
  } catch (e) {
    setMessage('Remove failed', 'error');
  } finally {
    loading.value = false;
  }
}

onMounted(load);
</script>
