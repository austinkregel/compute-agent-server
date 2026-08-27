<template>
  <div class="shadow rounded-lg bg-white dark:bg-gray-800 border border-transparent dark:border-gray-700 overflow-hidden">
    <div class="px-6 py-4 border-b border-gray-200 dark:border-gray-700 flex items-start justify-between gap-4">
      <div>
        <h2 class="text-lg font-medium text-gray-900 dark:text-gray-100">Backup</h2>
        <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
          Plan, review, and run a backup job on the selected agent.
        </p>
      </div>
      <div class="text-right">
        <div v-if="planId" class="text-xs text-gray-500 dark:text-gray-400">
          Plan: <span class="font-mono">{{ planId }}</span>
        </div>
        <div v-if="modeLabel" class="mt-1 text-xs">
          <span class="inline-flex items-center px-2 py-0.5 rounded-full border text-gray-600 dark:text-gray-200 border-gray-200 dark:border-gray-700">
            {{ modeLabel }}
          </span>
        </div>
      </div>
    </div>

    <div class="px-6 py-4 space-y-6">
      <!-- Source & destination connection -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div class="rounded-md border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/30 p-4">
          <div class="text-sm font-medium text-gray-800 dark:text-gray-200">Source connection</div>
          <div class="mt-3 grid grid-cols-1 sm:grid-cols-4 gap-4 items-end">
            <div class="sm:col-span-1">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Location</label>
              <select
                v-model="sourceMode"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              >
                <option value="local">Local (agent)</option>
                <option value="remote">Remote</option>
              </select>
            </div>
            <div class="sm:col-span-1" v-if="sourceMode === 'remote'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Protocol</label>
              <select
                v-model="sourceProtocol"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              >
                <option value="ssh">SSH</option>
                <option value="smb">SMB</option>
              </select>
            </div>
            <div class="sm:col-span-2" v-if="sourceMode === 'remote'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Host</label>
              <input
                v-model="sourceHost"
                placeholder="source-host"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
              <p v-if="sourceHostError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ sourceHostError }}</p>
            </div>

            <div class="sm:col-span-1" v-if="sourceMode === 'remote' && sourceProtocol === 'ssh'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">User (optional)</label>
              <input
                v-model="sourceUser"
                placeholder="ubuntu"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
            </div>
            <div class="sm:col-span-1" v-if="sourceMode === 'remote' && sourceProtocol === 'ssh'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Port</label>
              <input
                v-model.number="sourcePort"
                type="number"
                min="1"
                max="65535"
                placeholder="22"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              />
            </div>

            <div class="sm:col-span-1" v-if="sourceMode === 'remote' && sourceProtocol === 'smb'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Share</label>
              <input
                v-model="sourceShare"
                placeholder="share"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
              <p v-if="sourceShareError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ sourceShareError }}</p>
            </div>
            <div class="sm:col-span-1" v-if="sourceMode === 'remote' && sourceProtocol === 'smb'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">SMB profile</label>
              <input
                v-model="sourceProfile"
                placeholder="default"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
              <p v-if="sourceProfileError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ sourceProfileError }}</p>
            </div>
            <div class="sm:col-span-1" v-if="sourceMode === 'remote' && sourceProtocol === 'smb'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Port</label>
              <input
                v-model.number="sourcePort"
                type="number"
                min="1"
                max="65535"
                placeholder="445"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            Select where your source data lives. For SMB, the agent may mount the share and then treat it like a local rsync source.
          </p>
        </div>

        <div class="rounded-md border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/30 p-4">
          <div class="text-sm font-medium text-gray-800 dark:text-gray-200">Destination connection</div>
          <div class="mt-3 grid grid-cols-1 sm:grid-cols-4 gap-4 items-end">
            <div class="sm:col-span-1">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Location</label>
              <select
                v-model="destMode"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              >
                <option value="local">Local (agent)</option>
                <option value="remote">Remote</option>
              </select>
            </div>
            <div class="sm:col-span-1" v-if="destMode === 'remote'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Protocol</label>
              <select
                v-model="destProtocol"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              >
                <option value="ssh">SSH (rsync)</option>
                <option value="smb">SMB</option>
              </select>
            </div>
            <div class="sm:col-span-2" v-if="destMode === 'remote'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Host</label>
              <input
                v-model="destHost"
                placeholder="dest-host"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
              <p v-if="destHostError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ destHostError }}</p>
            </div>

            <div class="sm:col-span-1" v-if="destMode === 'remote' && destProtocol === 'ssh'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">User (optional)</label>
              <input
                v-model="destUser"
                placeholder="ubuntu"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
            </div>
            <div class="sm:col-span-1" v-if="destMode === 'remote' && destProtocol === 'ssh'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Port</label>
              <input
                v-model.number="destPort"
                type="number"
                min="1"
                max="65535"
                placeholder="22"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              />
            </div>

            <div class="sm:col-span-1" v-if="destMode === 'remote' && destProtocol === 'smb'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Share</label>
              <input
                v-model="destShare"
                placeholder="share"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
              <p v-if="destShareError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ destShareError }}</p>
            </div>
            <div class="sm:col-span-1" v-if="destMode === 'remote' && destProtocol === 'smb'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">SMB profile</label>
              <input
                v-model="destProfile"
                placeholder="default"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :disabled="planning || running"
              />
              <p v-if="destProfileError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ destProfileError }}</p>
            </div>
            <div class="sm:col-span-1" v-if="destMode === 'remote' && destProtocol === 'smb'">
              <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Port</label>
              <input
                v-model.number="destPort"
                type="number"
                min="1"
                max="65535"
                placeholder="445"
                class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed"
                :disabled="planning || running"
              />
            </div>
          </div>
          <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">
            SSH destination uses rsync. For SMB destinations, the agent may mount the share and then treat it like a local destination.
          </p>
        </div>
      </div>

      <!-- Source & destination -->
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <DirectoryBrowser
            v-model="sourceDirs"
            :client-id="clientId"
            :mode="sourceMode"
            type="source"
            :host="sourceHost"
            :user="sourceUser"
            :port="sourcePort"
            :protocol="sourceProtocol"
            :share="sourceShare"
            :profile="sourceProfile"
            title="Source directories (what you want copied)"
          />
          <p v-if="invalidSourceDirs" class="mt-2 text-xs text-red-600 dark:text-red-400">
            '*' is not allowed in source directories. Select explicit directory paths.
          </p>
          <p v-else-if="parsedSourceDirs.length === 0" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            Add at least one directory to back up.
          </p>
        </div>
        <div>
          <DirectoryBrowser
            v-model="destRoot"
            :client-id="clientId"
            :mode="destMode"
            type="destination"
            :host="destHost"
            :user="destUser"
            :port="destPort"
            :protocol="destProtocol"
            :share="destShare"
            :profile="destProfile"
            title="Destination (where it will be copied)"
          />
          <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
            <span v-if="destMode === 'remote'">Destination is on the remote host.</span>
            <span v-else>Destination is on the agent machine.</span>
          </p>
          <p v-if="destRootError" class="mt-2 text-xs text-red-600 dark:text-red-400">{{ destRootError }}</p>
        </div>
      </div>

      <!-- Options -->
      <div>
        <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">Ignored patterns</label>
        <textarea
          v-model="ignorePatternsInput"
          rows="5"
          placeholder="# One per line, like .gitignore\n**/node_modules/**\n**/.git/**\n*.log"
          class="mt-1 w-full px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm disabled:opacity-60 disabled:cursor-not-allowed font-mono"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          :disabled="planning || running"
        />
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          One pattern per line, similar to <span class="font-mono">.gitignore</span>. Blank lines and lines starting with <span class="font-mono">#</span> are ignored.
        </p>
      </div>

      <!-- Safety / intent -->
      <div class="rounded-md border border-amber-200 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20 p-4 text-xs text-amber-900 dark:text-amber-100">
        <div class="font-medium">Review what will happen</div>
        <div class="mt-1 space-y-1 text-amber-800 dark:text-amber-200">
          <div>
            - This will copy <b>{{ parsedSourceDirs.length }}</b> source folder(s) into <b class="font-mono">{{ destRoot || '(not selected yet)' }}</b>.
          </div>
          <div>
            - Nothing is copied until you click <b>Start backup</b>.
          </div>
          <div>
            - This can modify/overwrite files under the destination. Always plan first and review the plan summary.
          </div>
        </div>
      </div>

      <!-- Actions -->
      <div class="flex flex-wrap items-center gap-3">
        <button
          type="button"
          class="px-4 py-2 text-sm rounded-md bg-indigo-600 hover:bg-indigo-700 text-white disabled:opacity-50 disabled:cursor-not-allowed dark:bg-indigo-500 dark:hover:bg-indigo-400"
          :disabled="!canPlan"
          @click="requestPlan"
          :title="planDisabledReason"
        >
          {{ planning ? 'Planning…' : 'Plan backup' }}
        </button>
        <button
          type="button"
          class="px-4 py-2 text-sm rounded-md bg-green-600 hover:bg-green-700 text-white disabled:opacity-50 disabled:cursor-not-allowed dark:bg-green-500 dark:hover:bg-green-400"
          :disabled="!canApprove"
          @click="approve"
        >
          Start backup
        </button>
        <button
          type="button"
          class="px-4 py-2 text-sm rounded-md bg-gray-200 hover:bg-gray-300 text-gray-800 disabled:opacity-50 disabled:cursor-not-allowed dark:bg-gray-700 dark:hover:bg-gray-600 dark:text-gray-100"
          :disabled="planning || running"
          @click="resetForm"
        >
          Reset
        </button>

        <div class="ml-auto text-xs text-gray-500 dark:text-gray-400" v-if="planning || running">
          <span v-if="planning">Waiting for plan response…</span>
          <span v-else>Backup running…</span>
        </div>
      </div>

      <div v-if="plan && !running" class="mt-1 flex items-start gap-2 text-xs text-gray-700 dark:text-gray-200">
        <input id="confirmWrites" type="checkbox" v-model="confirmWrites" class="mt-1" :disabled="planning || running" />
        <label for="confirmWrites" class="select-none">
          I understand this backup may modify/overwrite files under the destination. I have reviewed the plan summary.
        </label>
      </div>

      <!-- Plan summary -->
      <div v-if="plan" class="rounded-md border border-gray-200 dark:border-gray-700 p-4 bg-gray-50 dark:bg-gray-900/40">
        <div class="flex items-start justify-between gap-4">
          <div>
            <div class="text-sm font-medium text-gray-800 dark:text-gray-200">Plan summary</div>
            <div class="mt-1 text-xs text-gray-600 dark:text-gray-300">
              Total files: <b>{{ plan.totalFiles }}</b> · Total bytes: <b>{{ humanBytes(plan.totalBytes) }}</b>
              <span v-if="Array.isArray(plan.modifies) && plan.modifies.length"> · Modifies: <b>{{ plan.modifies.length }}</b></span>
            </div>
          </div>
          <button
            type="button"
            class="text-xs px-2 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-white dark:hover:bg-gray-800"
            @click="toggleSample"
          >
            {{ showSample ? 'Hide sample' : 'Show sample' }}
          </button>
        </div>
        <div v-if="showSample && Array.isArray(plan.files) && plan.files.length" class="mt-3">
          <div class="text-xs text-gray-500 dark:text-gray-400 mb-2">Sample files (up to {{ plan.files.length }})</div>
          <ul class="text-xs font-mono space-y-1 max-h-40 overflow-auto">
            <li v-for="f in plan.files" :key="f" class="text-gray-700 dark:text-gray-200 truncate" :title="f">{{ f }}</li>
          </ul>
        </div>
      </div>

      <!-- Progress -->
      <div v-if="running || percent > 0" class="space-y-2">
        <div class="h-2 w-full rounded bg-gray-200 dark:bg-gray-700 overflow-hidden">
          <div class="h-2 bg-green-500 dark:bg-green-400" :style="{ width: safePercent + '%' }"></div>
        </div>
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs text-gray-600 dark:text-gray-300">
          <div>
            <b>{{ safePercent.toFixed(0) }}%</b>
            · {{ humanBytes(transferredBytes) }}
            <span v-if="plan?.totalBytes">/ {{ humanBytes(plan.totalBytes) }}</span>
            transferred
            <span v-if="speedBps > 0"> · {{ humanBytes(speedBps) }}/s</span>
          </div>
          <div class="sm:text-right">
            Files completed: <b>{{ filesCompleted }}</b>
            <span v-if="etaSeconds != null"> · ETA: <b>{{ formatEta(etaSeconds) }}</b></span>
          </div>
        </div>
        <div v-if="lastFile" class="text-xs text-gray-500 dark:text-gray-400">
          Last:
          <span v-if="lastOp" class="font-mono">[{{ lastOp }}]</span>
          <span class="font-mono truncate inline-block align-bottom max-w-full" :title="lastFile">{{ lastFile }}</span>
        </div>
      </div>

      <!-- Errors -->
      <div
        v-if="error"
        class="p-3 rounded-md border text-xs bg-red-50 border-red-200 text-red-700 dark:bg-red-900/40 dark:border-red-700 dark:text-red-300"
      >
        <div class="flex items-start justify-between gap-3">
          <div>
            <div class="font-medium">Backup error</div>
            <div class="mt-1 break-words">{{ error }}</div>
          </div>
          <button
            type="button"
            class="shrink-0 px-2 py-1 rounded border border-red-300 dark:border-red-700/70 text-red-700 dark:text-red-200 hover:bg-red-100 dark:hover:bg-red-900/60"
            @click="error = ''"
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { send, on as onWS } from '../lib/sharedWS.js';
import DirectoryBrowser from './DirectoryBrowser.vue';

const props = defineProps({ clientId: { type: String, default: '' } });

// Source endpoint (where the data lives)
const sourceMode = ref('local'); // 'local' | 'remote'
const sourceProtocol = ref('ssh'); // 'ssh' | 'smb'
const sourceHost = ref('');
const sourceUser = ref('');
const sourcePort = ref(22);
const sourceShare = ref('');
const sourceProfile = ref('');

// Destination endpoint (where data is copied to)
const destMode = ref('local'); // 'local' | 'remote'
const destProtocol = ref('ssh'); // 'ssh' | 'smb'
const destHost = ref('');
const destUser = ref('');
const destPort = ref(22);
const destShare = ref('');
const destProfile = ref('');

const sourceDirs = ref([]);
const destRoot = ref('');
const ignorePatternsInput = ref('**/node_modules/**\n**/.git/**');
const confirmWrites = ref(false);

const planId = ref('');
const plan = ref(null);
const planning = ref(false);
const running = ref(false);
const percent = ref(0);
const transferredBytes = ref(0);
const filesCompleted = ref(0);
const lastFile = ref('');
const lastOp = ref('');
const error = ref('');
const showSample = ref(false);

const lastProgressAtMs = ref(0);
const lastProgressBytes = ref(0);
const speedBps = ref(0);

const canApprove = computed(() =>
  props.clientId && planId.value && plan.value && !running.value && !planning.value && confirmWrites.value
);
const parsedSourceDirs = computed(() =>
  (Array.isArray(sourceDirs.value) ? sourceDirs.value : [])
    .map(s => String(s || '').trim())
    .filter(Boolean)
);
const invalidSourceDirs = computed(() => parsedSourceDirs.value.some(s => s.includes('*')));

const destRootError = computed(() => {
  if (planning.value || running.value) return '';
  if (!destRoot.value.trim()) return 'Destination root is required.';
  return '';
});

const sourceHostError = computed(() => {
  if (sourceMode.value !== 'remote') return '';
  if (planning.value || running.value) return '';
  if (!sourceHost.value.trim()) return 'Host is required for remote source.';
  return '';
});
const sourceShareError = computed(() => {
  if (sourceMode.value !== 'remote' || sourceProtocol.value !== 'smb') return '';
  if (planning.value || running.value) return '';
  if (!sourceShare.value.trim()) return 'Share is required for SMB source.';
  return '';
});
const sourceProfileError = computed(() => {
  if (sourceMode.value !== 'remote' || sourceProtocol.value !== 'smb') return '';
  if (planning.value || running.value) return '';
  if (!sourceProfile.value.trim()) return 'Profile is required for SMB source.';
  return '';
});

const destHostError = computed(() => {
  if (destMode.value !== 'remote') return '';
  if (planning.value || running.value) return '';
  if (!destHost.value.trim()) return 'Host is required for remote destination.';
  return '';
});
const destShareError = computed(() => {
  if (destMode.value !== 'remote' || destProtocol.value !== 'smb') return '';
  if (planning.value || running.value) return '';
  if (!destShare.value.trim()) return 'Share is required for SMB destination.';
  return '';
});
const destProfileError = computed(() => {
  if (destMode.value !== 'remote' || destProtocol.value !== 'smb') return '';
  if (planning.value || running.value) return '';
  if (!destProfile.value.trim()) return 'Profile is required for SMB destination.';
  return '';
});

const canPlan = computed(() => {
  if (!props.clientId) return false;
  if (planning.value || running.value) return false;
  if (invalidSourceDirs.value) return false;
  if (parsedSourceDirs.value.length === 0) return false;
  if (!destRoot.value.trim()) return false;
  if (sourceMode.value === 'remote') {
    if (!sourceHost.value.trim()) return false;
    if (sourceProtocol.value === 'smb' && (!sourceShare.value.trim() || !sourceProfile.value.trim())) return false;
  }
  if (destMode.value === 'remote') {
    if (!destHost.value.trim()) return false;
    if (destProtocol.value === 'smb' && (!destShare.value.trim() || !destProfile.value.trim())) return false;
  }
  return true;
});

const planDisabledReason = computed(() => {
  if (!props.clientId) return 'Select a client first';
  if (planning.value) return 'Already planning';
  if (running.value) return 'Backup is running';
  if (invalidSourceDirs.value) return 'Source directories must be explicit (no *)';
  if (parsedSourceDirs.value.length === 0) return 'Provide at least one source directory';
  if (!destRoot.value.trim()) return 'Destination root is required';
  if (sourceMode.value === 'remote' && !sourceHost.value.trim()) return 'Source host is required';
  if (sourceMode.value === 'remote' && sourceProtocol.value === 'smb' && !sourceShare.value.trim()) return 'Source SMB share is required';
  if (sourceMode.value === 'remote' && sourceProtocol.value === 'smb' && !sourceProfile.value.trim()) return 'Source SMB profile is required';
  if (destMode.value === 'remote' && !destHost.value.trim()) return 'Destination host is required';
  if (destMode.value === 'remote' && destProtocol.value === 'smb' && !destShare.value.trim()) return 'Destination SMB share is required';
  if (destMode.value === 'remote' && destProtocol.value === 'smb' && !destProfile.value.trim()) return 'Destination SMB profile is required';
  return '';
});

const safePercent = computed(() => {
  const p = Number(percent.value);
  if (!isFinite(p)) return 0;
  return Math.max(0, Math.min(100, p));
});

const etaSeconds = computed(() => {
  const total = Number(plan.value?.totalBytes);
  if (!isFinite(total) || total <= 0) return null;
  const bps = Number(speedBps.value);
  if (!isFinite(bps) || bps <= 0) return null;
  const remaining = Math.max(0, total - Number(transferredBytes.value || 0));
  return Math.floor(remaining / bps);
});

const modeLabel = computed(() => {
  const s = sourceMode.value === 'remote' ? `Source:${sourceProtocol.value}` : 'Source:local';
  const d = destMode.value === 'remote' ? `Dest:${destProtocol.value}` : 'Dest:local';
  return `${s} · ${d}`;
});

function humanBytes(v) {
  if (!v || isNaN(v)) return v;
  const units = ['B','KB','MB','GB','TB'];
  let i=0; let n=Number(v);
  while (n>=1024 && i<units.length-1){ n/=1024; i++; }
  return n.toFixed(n>=10?0:1)+units[i];
}

function formatEta(sec) {
  if (sec == null || !isFinite(sec)) return '';
  const s = Math.max(0, Math.floor(sec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = s % 60;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${ss}s`;
  return `${ss}s`;
}

watch(sourceProtocol, (p) => {
  if (sourceMode.value !== 'remote') return;
  const proto = String(p || '').trim().toLowerCase();
  if (proto === 'smb') {
    sourceUser.value = '';
    if (!sourcePort.value || sourcePort.value === 22) sourcePort.value = 445;
  } else {
    if (!sourcePort.value || sourcePort.value === 445) sourcePort.value = 22;
  }
});
watch(destProtocol, (p) => {
  if (destMode.value !== 'remote') return;
  const proto = String(p || '').trim().toLowerCase();
  if (proto === 'smb') {
    destUser.value = '';
    if (!destPort.value || destPort.value === 22) destPort.value = 445;
  } else {
    if (!destPort.value || destPort.value === 445) destPort.value = 22;
  }
});

function resetEphemeralState() {
  planId.value = '';
  plan.value = null;
  showSample.value = false;
  confirmWrites.value = false;
  percent.value = 0;
  transferredBytes.value = 0;
  filesCompleted.value = 0;
  lastFile.value = '';
  lastOp.value = '';
  error.value = '';
  speedBps.value = 0;
  lastProgressAtMs.value = 0;
  lastProgressBytes.value = 0;
}

function resetForm() {
  sourceMode.value = 'local';
  sourceProtocol.value = 'ssh';
  sourceHost.value = '';
  sourceUser.value = '';
  sourcePort.value = 22;
  sourceShare.value = '';
  sourceProfile.value = '';

  destMode.value = 'local';
  destProtocol.value = 'ssh';
  destHost.value = '';
  destUser.value = '';
  destPort.value = 22;
  destShare.value = '';
  destProfile.value = '';

  sourceDirs.value = [];
  destRoot.value = '';
  ignorePatternsInput.value = '**/node_modules/**\n**/.git/**';
  resetEphemeralState();
}

function toggleSample() {
  showSample.value = !showSample.value;
}

function requestPlan(){
  resetEphemeralState();
  const sourceDirs = parsedSourceDirs.value;
  if (invalidSourceDirs.value) return;
  const ignoreGlobs = ignorePatternsInput.value
    .split(/\r?\n/g)
    .map(s => s.trim())
    .filter(Boolean)
    .filter(s => !s.startsWith('#'));
  planning.value = true;
  const source = {
    mode: sourceMode.value,
    protocol: sourceMode.value === 'remote' ? sourceProtocol.value : '',
    host: sourceMode.value === 'remote' ? sourceHost.value : '',
    user: sourceMode.value === 'remote' ? sourceUser.value : '',
    port: sourceMode.value === 'remote' ? sourcePort.value : 0,
    share: (sourceMode.value === 'remote' && sourceProtocol.value === 'smb') ? sourceShare.value : '',
    profile: (sourceMode.value === 'remote' && sourceProtocol.value === 'smb') ? sourceProfile.value : '',
  };
  const destination = {
    mode: destMode.value,
    protocol: destMode.value === 'remote' ? destProtocol.value : '',
    host: destMode.value === 'remote' ? destHost.value : '',
    user: destMode.value === 'remote' ? destUser.value : '',
    port: destMode.value === 'remote' ? destPort.value : 0,
    share: (destMode.value === 'remote' && destProtocol.value === 'smb') ? destShare.value : '',
    profile: (destMode.value === 'remote' && destProtocol.value === 'smb') ? destProfile.value : '',
  };
  send({
    type: 'backup_plan_request',
    clientId: props.clientId,
    // Legacy fields (kept for backwards compatibility with older agents):
    // - Historically, host/user/port represented the destination SSH host for rsync.
    host: (destination.mode === 'remote' && destination.protocol === 'ssh') ? destination.host : '',
    user: (destination.mode === 'remote' && destination.protocol === 'ssh') ? destination.user : '',
    port: (destination.mode === 'remote' && destination.protocol === 'ssh') ? destination.port : 22,

    // New structured endpoints (for SMB mount + local rsync workflows, etc.)
    source,
    destination,
    sourceDirs,
    destRoot: destRoot.value,
    ignoreGlobs,
  });
}
function approve(){
  if (!planId.value) return;
  if (!confirmWrites.value) return;
  running.value = true;
  error.value = '';
  percent.value = 0;
  transferredBytes.value = 0;
  filesCompleted.value = 0;
  lastFile.value = '';
  lastOp.value = '';
  speedBps.value = 0;
  lastProgressAtMs.value = 0;
  lastProgressBytes.value = 0;
  send({ type: 'backup_approve', planId: planId.value });
}

const offs = [];
onMounted(()=>{
  offs.push(onWS('backup_plan_dispatched', (m)=>{ if (m?.clientId === props.clientId) { planId.value = m.planId; } }));
  offs.push(onWS('backup_plan', (m)=>{ if (m?.clientId === props.clientId) { if (!planId.value) planId.value = m.planId; plan.value = m.plan; planning.value = false; } }));
  offs.push(onWS('backup_started', (m)=>{ if (m?.clientId === props.clientId) running.value = true; }));
  offs.push(onWS('backup_progress', (m)=>{ if (m?.clientId === props.clientId && m.planId === planId.value) {
    if (typeof m.percent === 'number') percent.value = m.percent;
    if (typeof m.transferredBytes === 'number') transferredBytes.value = m.transferredBytes;
    if (typeof m.filesCompleted === 'number') filesCompleted.value = m.filesCompleted;
    if (m.file) lastFile.value = m.file;
    if (m.op) lastOp.value = m.op;
    const now = Date.now();
    if (typeof m.transferredBytes === 'number') {
      const lastAt = lastProgressAtMs.value;
      const lastBytes = lastProgressBytes.value;
      if (lastAt && now > lastAt && m.transferredBytes >= lastBytes) {
        const dt = (now - lastAt) / 1000;
        const db = m.transferredBytes - lastBytes;
        if (dt > 0) {
          // Simple smoothing: 70% previous, 30% new.
          const inst = db / dt;
          speedBps.value = speedBps.value ? (speedBps.value * 0.7 + inst * 0.3) : inst;
        }
      }
      lastProgressAtMs.value = now;
      lastProgressBytes.value = m.transferredBytes;
    }
  }}));
  offs.push(onWS('backup_complete', (m)=>{ if (m?.clientId === props.clientId) { running.value = false; percent.value = 100; } }));
  offs.push(onWS('backup_error', (m)=>{ if (m?.clientId === props.clientId) { running.value = false; planning.value = false; error.value = m.error || 'error'; } }));
});
onUnmounted(()=> offs.forEach(fn=>fn()));
</script>

