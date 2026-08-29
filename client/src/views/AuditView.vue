<template>
  <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6 space-y-6">
    <!-- Header -->
    <div>
      <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Access Audit</h1>
      <p class="text-sm text-gray-500 dark:text-gray-400 mt-1">
        Tamper-evident record of who authenticated, who was denied, and every privileged action
        taken against a managed machine. Records are hash-chained, so removing or editing one
        breaks the chain visibly.
      </p>
    </div>

    <!-- Chain integrity, shown before the records themselves. -->
    <div
      v-if="chain"
      :class="['rounded-lg border p-3', chain.valid
        ? 'border-green-300 bg-green-50 dark:border-green-700 dark:bg-green-900/30'
        : 'border-red-400 bg-red-50 dark:border-red-700 dark:bg-red-900/30']"
    >
      <p v-if="chain.valid" class="text-sm font-medium text-green-800 dark:text-green-300">
        Chain intact — {{ chain.count }} record{{ chain.count === 1 ? '' : 's' }} verified.
      </p>
      <template v-else>
        <p class="text-sm font-semibold text-red-800 dark:text-red-300">
          ⚠ Chain broken at record #{{ chain.brokenAt }}
        </p>
        <p class="text-xs text-red-700 dark:text-red-400 mt-1">
          {{ chain.reason }} — the audit log has been altered since it was written. Treat everything
          after this point as unreliable.
        </p>
      </template>
    </div>

    <!-- First-seen accesses, surfaced separately from the main stream. -->
    <div
      v-if="firstSeen.length"
      class="rounded-lg border border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-900/30 p-3"
    >
      <p class="text-sm font-medium text-amber-800 dark:text-amber-300">
        {{ firstSeen.length }} first-time access{{ firstSeen.length === 1 ? '' : 'es' }} in this window
      </p>
      <ul class="mt-2 space-y-1">
        <li v-for="e in firstSeen" :key="e.seq" class="text-xs text-amber-700 dark:text-amber-400 font-mono">
          {{ formatTime(e.time) }} · {{ e.actorName || e.actor }} from {{ e.remote || 'unknown' }}
        </li>
      </ul>
    </div>

    <!-- Filters -->
    <div class="flex flex-wrap items-center gap-2">
      <select
        v-model="typeFilter"
        class="text-xs rounded-md border border-gray-300 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100 px-3 py-2"
      >
        <option value="">All event types</option>
        <option v-for="t in TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
      </select>
      <select
        v-model.number="limit"
        class="text-xs rounded-md border border-gray-300 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100 px-3 py-2"
      >
        <option :value="100">Last 100</option>
        <option :value="500">Last 500</option>
        <option :value="2000">Last 2000</option>
      </select>
      <button
        @click="load"
        type="button"
        :disabled="loading"
        class="px-3 py-2 text-xs rounded-md bg-indigo-600 hover:bg-indigo-700 text-white disabled:opacity-50 transition-colors"
      >{{ loading ? 'Loading…' : 'Refresh' }}</button>
    </div>

    <div v-if="error" class="p-3 rounded-md text-sm bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300">
      {{ error }}
    </div>

    <!-- Records, most recent first -->
    <div class="rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden">
      <div class="overflow-x-auto">
        <table class="min-w-full text-xs">
          <thead class="bg-gray-50 dark:bg-gray-900/50">
            <tr class="text-left text-gray-500 dark:text-gray-400">
              <th class="px-3 py-2 font-medium">#</th>
              <th class="px-3 py-2 font-medium">Time</th>
              <th class="px-3 py-2 font-medium">Event</th>
              <th class="px-3 py-2 font-medium">Actor</th>
              <th class="px-3 py-2 font-medium">From</th>
              <th class="px-3 py-2 font-medium">Machine</th>
              <th class="px-3 py-2 font-medium">Action</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
            <tr v-if="!rows.length && !loading">
              <td colspan="7" class="px-3 py-6 text-center text-gray-500 dark:text-gray-400">
                No records in this window.
              </td>
            </tr>
            <tr
              v-for="e in rows"
              :key="e.seq"
              :class="rowClass(e)"
            >
              <td class="px-3 py-2 font-mono text-gray-400 dark:text-gray-500">{{ e.seq }}</td>
              <td class="px-3 py-2 font-mono whitespace-nowrap text-gray-600 dark:text-gray-300">{{ formatTime(e.time) }}</td>
              <td class="px-3 py-2">
                <span :class="badgeClass(e)">{{ typeLabel(e.type) }}</span>
              </td>
              <td class="px-3 py-2 text-gray-900 dark:text-gray-100">
                {{ e.actorName || e.actor || '—' }}
                <span v-if="e.groups?.length" class="text-gray-400 dark:text-gray-500">
                  ({{ e.groups.join(', ') }})
                </span>
              </td>
              <td class="px-3 py-2 font-mono text-gray-600 dark:text-gray-300">{{ e.remote || '—' }}</td>
              <td class="px-3 py-2 font-mono text-gray-600 dark:text-gray-300">{{ e.clientId || '—' }}</td>
              <td class="px-3 py-2 font-mono text-gray-600 dark:text-gray-300">
                {{ e.action || '—' }}
                <span v-if="detailSummary(e)" class="text-gray-400 dark:text-gray-500">{{ detailSummary(e) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue';

const TYPES = [
  { value: 'first_seen', label: 'First seen' },
  { value: 'login_success', label: 'Login' },
  { value: 'login_failure', label: 'Login denied' },
  { value: 'dashboard_connect', label: 'Dashboard connect' },
  { value: 'agent_connect', label: 'Agent connect' },
  { value: 'agent_auth_failure', label: 'Agent auth failed' },
  { value: 'admin_action', label: 'Admin action' },
  { value: 'admin_denied', label: 'Admin denied' },
  { value: 'privileged_event', label: 'Privileged event' },
  { value: 'event_denied', label: 'Event denied' },
  { value: 'allowlist_change', label: 'Allowlist change' },
];

const events = ref([]);
const chain = ref(null);
const loading = ref(false);
const error = ref('');
const typeFilter = ref('');
const limit = ref(500);

// The API returns oldest first; display newest first.
const rows = computed(() => [...events.value].reverse());
const firstSeen = computed(() => rows.value.filter((e) => e.type === 'first_seen'));

function typeLabel(t) {
  return TYPES.find((x) => x.value === t)?.label || t;
}

function formatTime(t) {
  if (!t) return '—';
  const d = new Date(t);
  return Number.isNaN(d.getTime()) ? t : d.toLocaleString();
}

function badgeClass(e) {
  const base = 'inline-block px-1.5 py-0.5 rounded font-medium';
  if (e.type === 'first_seen') return `${base} bg-amber-100 text-amber-800 dark:bg-amber-900/50 dark:text-amber-300`;
  if (e.outcome === 'deny') return `${base} bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-300`;
  return `${base} bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300`;
}

function rowClass(e) {
  if (e.type === 'first_seen') return 'bg-amber-50/60 dark:bg-amber-900/20';
  if (e.outcome === 'deny') return 'bg-red-50/60 dark:bg-red-900/20';
  return '';
}

function detailSummary(e) {
  if (!e.detail) return '';
  const parts = [];
  for (const k of ['path', 'command', 'reason', 'added', 'removed']) {
    if (e.detail[k] !== undefined) parts.push(`${k}=${e.detail[k]}`);
  }
  return parts.length ? ` · ${parts.join(' ')}` : '';
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const params = new URLSearchParams({ limit: String(limit.value) });
    if (typeFilter.value) params.set('type', typeFilter.value);
    const res = await fetch(`/api/audit?${params}`, { credentials: 'include' });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || `request failed (${res.status})`);
    }
    const data = await res.json();
    events.value = data.events || [];
    chain.value = data.chain || null;
  } catch (err) {
    error.value = err.message;
  } finally {
    loading.value = false;
  }
}

watch([typeFilter, limit], load);
onMounted(load);
</script>
