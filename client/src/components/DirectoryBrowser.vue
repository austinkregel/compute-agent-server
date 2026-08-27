<template>
  <div class="rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900/30 p-3">
    <div class="flex items-start justify-between gap-3">
      <div>
        <div class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ title }}</div>
        <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          <span v-if="mode === 'remote'">
            Browsing remote filesystem via <span class="font-mono">{{ remoteProtocol }}</span> from the agent.
          </span>
          <span v-else>Browsing the agent filesystem.</span>
        </div>
      </div>

      <div class="text-right text-xs">
        <span
          v-if="mode === 'remote'"
          class="inline-flex items-center px-2 py-0.5 rounded-full border"
          :class="validationBadgeClass"
        >
          {{ validationBadgeText }}
        </span>
      </div>
    </div>

    <div v-if="mode === 'remote' && !canBrowse" class="mt-3 text-xs text-gray-600 dark:text-gray-300">
      <span v-if="!hostTrimmed">Enter a remote host above to browse directories.</span>
      <span v-else-if="remoteProtocol === 'smb' && !shareTrimmed">Enter an SMB share name above to browse directories.</span>
      <span v-else-if="remoteProtocol === 'smb' && !profileTrimmed">Enter an SMB profile name above to browse directories.</span>
      <span v-else>Not ready to browse.</span>
    </div>

    <div class="mt-3 flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="!canBrowse || currentPath === '/' || loading"
        @click="goUp"
      >
        Up
      </button>
      <button
        type="button"
        class="px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="!canBrowse || currentPath === '/' || loading"
        @click="goRoot"
      >
        Root
      </button>

      <div class="flex-1 min-w-[240px]">
        <div class="text-[11px] text-gray-500 dark:text-gray-400 truncate" :title="currentPath">
          <span v-for="(seg, idx) in breadcrumb" :key="idx">
            <button
              v-if="seg.path"
              type="button"
              class="hover:underline"
              :disabled="loading || !canBrowse"
              @click="navigate(seg.path)"
            >
              {{ seg.label }}
            </button>
            <span v-else>{{ seg.label }}</span>
            <span v-if="idx < breadcrumb.length - 1" class="text-gray-400 dark:text-gray-500"> / </span>
          </span>
        </div>
      </div>

      <button
        type="button"
        class="px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="!canBrowse"
        @click="manual = !manual"
      >
        {{ manual ? 'Hide manual path' : 'Manual path' }}
      </button>

      <button
        type="button"
        class="px-2 py-1 text-xs rounded bg-indigo-600 hover:bg-indigo-700 text-white disabled:opacity-50 disabled:cursor-not-allowed dark:bg-indigo-500 dark:hover:bg-indigo-400"
        :disabled="!canBrowse || loading"
        @click="selectCurrentFolder"
        :title="isMulti ? 'Add current folder to sources' : 'Select current folder as destination'"
      >
        {{ isMulti ? 'Add this folder' : 'Select this folder' }}
      </button>
    </div>

    <div v-if="manual" class="mt-3 flex items-center gap-2">
      <input
        v-model="manualPath"
        placeholder="/absolute/path"
        class="flex-1 px-3 py-2 rounded-md border border-gray-300 dark:border-gray-600 shadow-sm focus:ring-indigo-500 focus:border-indigo-500 bg-white dark:bg-gray-900 dark:text-gray-100 text-sm"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :disabled="!canBrowse || loading"
        @keydown.enter.prevent="goManual"
      />
      <button
        type="button"
        class="px-3 py-2 text-sm rounded-md border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="!canBrowse || loading"
        @click="goManual"
      >
        Go
      </button>
      <button
        v-if="manualPathNormalized"
        type="button"
        class="px-3 py-2 text-sm rounded-md border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="!canBrowse || loading"
        @click="selectManual"
        :title="isMulti ? 'Add this path to sources' : 'Use this path as destination'"
      >
        Use
      </button>
    </div>

    <div v-if="error" class="mt-3 text-xs p-2 rounded border bg-red-50 border-red-200 text-red-700 dark:bg-red-900/30 dark:border-red-700 dark:text-red-200">
      {{ error }}
    </div>

    <div class="mt-3 rounded border border-gray-200 dark:border-gray-700 overflow-hidden">
      <div class="max-h-64 overflow-auto">
        <table class="w-full text-xs">
          <thead class="bg-gray-50 dark:bg-gray-800/60">
            <tr>
              <th class="text-left px-3 py-2 font-medium text-gray-600 dark:text-gray-300">Name</th>
              <th class="text-right px-3 py-2 font-medium text-gray-600 dark:text-gray-300 w-20">Size</th>
              <th class="text-right px-3 py-2 font-medium text-gray-600 dark:text-gray-300 w-28">Modified</th>
              <th class="text-center px-3 py-2 font-medium text-gray-600 dark:text-gray-300 w-24">Perms</th>
              <th v-if="enableFileOps && mode === 'local'" class="text-center px-3 py-2 font-medium text-gray-600 dark:text-gray-300 w-24">Actions</th>
            </tr>
          </thead>
          <tbody class="bg-white dark:bg-gray-900/20">
            <tr v-if="!canBrowse" class="border-t border-gray-200 dark:border-gray-800">
              <td class="px-3 py-3 text-gray-500 dark:text-gray-400" :colspan="enableFileOps && mode === 'local' ? 5 : 4">Not ready to browse.</td>
            </tr>
            <tr v-else-if="loading" class="border-t border-gray-200 dark:border-gray-800">
              <td class="px-3 py-3 text-gray-500 dark:text-gray-400" :colspan="enableFileOps && mode === 'local' ? 5 : 4">Loading…</td>
            </tr>
            <tr v-else-if="entries.length === 0" class="border-t border-gray-200 dark:border-gray-800">
              <td class="px-3 py-3 text-gray-500 dark:text-gray-400" :colspan="enableFileOps && mode === 'local' ? 5 : 4">(Empty)</td>
            </tr>

            <tr
              v-for="e in entries"
              :key="e.name + ':' + e.type"
              class="border-t border-gray-100 dark:border-gray-800 hover:bg-gray-50 dark:hover:bg-gray-800/40 cursor-pointer"
              @click="onEntryClick(e)"
              @dblclick.prevent="onEntryDblClick(e)"
            >
              <td class="px-3 py-2">
                <div class="flex items-center gap-2">
                  <span class="text-[10px] px-1.5 py-0.5 rounded border border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-300 bg-gray-50 dark:bg-gray-800/40">
                    {{ e.isSymlink ? 'LINK' : (e.type === 'dir' ? 'DIR' : 'FILE') }}
                  </span>
                  <span class="text-gray-800 dark:text-gray-200" :title="e.linkTarget ? `${e.name} → ${e.linkTarget}` : e.name">
                    {{ e.name }}<span v-if="e.isSymlink && e.linkTarget" class="text-gray-400 dark:text-gray-500 ml-1">→ {{ truncateLinkTarget(e.linkTarget) }}</span>
                  </span>
                  <span
                    v-if="e.type === 'dir' && isSelectedDir(e)"
                    class="ml-auto text-[11px] px-2 py-0.5 rounded-full border border-green-300 text-green-700 bg-green-50 dark:border-green-700 dark:text-green-200 dark:bg-green-900/30"
                  >
                    selected
                  </span>
                </div>
              </td>
              <td class="px-3 py-2 text-right text-gray-500 dark:text-gray-400 font-mono">{{ formatSize(e.size) }}</td>
              <td class="px-3 py-2 text-right text-gray-500 dark:text-gray-400">{{ formatModTime(e.modTime) }}</td>
              <td class="px-3 py-2 text-center text-gray-500 dark:text-gray-400 font-mono text-[10px]">{{ e.mode || '-' }}</td>
              <td v-if="enableFileOps && mode === 'local'" class="px-3 py-2 text-center">
                <div class="flex items-center justify-center gap-1">
                  <button
                    v-if="e.type !== 'dir'"
                    type="button"
                    class="p-1 text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"
                    title="Change permissions"
                    @click.stop="openChmodDialog(e)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z" />
                    </svg>
                  </button>
                  <button
                    type="button"
                    class="p-1 text-gray-400 hover:text-red-600 dark:hover:text-red-400"
                    title="Delete"
                    @click.stop="openDeleteDialog(e)"
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                    </svg>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="mt-3" v-if="selectedList.length">
      <div class="text-xs text-gray-500 dark:text-gray-400 mb-2">Selected</div>
      <div class="flex flex-wrap gap-2">
        <span
          v-for="p in selectedList"
          :key="p"
          class="inline-flex items-center gap-2 px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-600 bg-gray-50 dark:bg-gray-800/40 text-gray-800 dark:text-gray-100"
        >
          <span class="font-mono" :title="p">{{ p }}</span>
          <button
            type="button"
            class="text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-100"
            @click.stop="removeSelected(p)"
            title="Remove"
          >
            ×
          </button>
        </span>
      </div>
    </div>

    <!-- File operations toolbar -->
    <div v-if="enableFileOps && mode === 'local' && canBrowse" class="mt-3 flex items-center gap-2 border-t border-gray-200 dark:border-gray-700 pt-3">
      <input
        ref="fileInput"
        type="file"
        class="hidden"
        @change="handleFileSelect"
        multiple
      />
      <button
        type="button"
        class="px-3 py-1.5 text-xs rounded bg-indigo-600 hover:bg-indigo-700 text-white disabled:opacity-50 dark:bg-indigo-500 dark:hover:bg-indigo-400"
        :disabled="uploading"
        @click="$refs.fileInput.click()"
      >
        {{ uploading ? 'Uploading...' : 'Upload Files' }}
      </button>
      <span v-if="uploadProgress" class="text-xs text-gray-500 dark:text-gray-400">{{ uploadProgress }}</span>
      <span v-if="fileOpMessage" :class="fileOpMessageClass" class="text-xs ml-auto">{{ fileOpMessage }}</span>
    </div>

    <!-- Delete confirmation modal -->
    <div v-if="deleteDialog.show" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" @click.self="closeDeleteDialog">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full mx-4 p-6">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">Confirm Delete</h3>
        <p class="text-sm text-gray-600 dark:text-gray-300 mb-2">
          Are you sure you want to delete:
        </p>
        <p class="text-sm font-mono bg-gray-100 dark:bg-gray-700 p-2 rounded mb-4 break-all">{{ deleteDialog.path }}</p>
        
        <div v-if="deleteDialog.isDangerous" class="mb-4 p-3 bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-700 rounded">
          <p class="text-sm text-amber-800 dark:text-amber-200 font-medium">Warning: Dangerous Path</p>
          <p class="text-xs text-amber-700 dark:text-amber-300 mt-1">This path is in a system directory. Deleting it may cause system instability.</p>
          <label class="flex items-center gap-2 mt-2 text-sm text-amber-800 dark:text-amber-200">
            <input type="checkbox" v-model="deleteDialog.forceConfirmed" class="rounded border-amber-400" />
            I understand the risks and want to proceed
          </label>
        </div>

        <div v-if="deleteDialog.isDir" class="mb-4">
          <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300">
            <input type="checkbox" v-model="deleteDialog.recursive" class="rounded border-gray-300 dark:border-gray-600" />
            Delete recursively (remove all contents)
          </label>
        </div>

        <div class="flex justify-end gap-2">
          <button
            type="button"
            class="px-4 py-2 text-sm rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700"
            @click="closeDeleteDialog"
          >
            Cancel
          </button>
          <button
            type="button"
            class="px-4 py-2 text-sm rounded bg-red-600 hover:bg-red-700 text-white disabled:opacity-50"
            :disabled="deleteDialog.isDangerous && !deleteDialog.forceConfirmed"
            @click="confirmDelete"
          >
            Delete
          </button>
        </div>
      </div>
    </div>

    <!-- Chmod modal -->
    <div v-if="chmodDialog.show" class="fixed inset-0 bg-black/50 flex items-center justify-center z-50" @click.self="closeChmodDialog">
      <div class="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-md w-full mx-4 p-6">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">Change Permissions</h3>
        <p class="text-sm text-gray-600 dark:text-gray-300 mb-2">File:</p>
        <p class="text-sm font-mono bg-gray-100 dark:bg-gray-700 p-2 rounded mb-4 break-all">{{ chmodDialog.path }}</p>
        
        <div class="mb-4">
          <label class="block text-sm text-gray-600 dark:text-gray-300 mb-1">New permissions (octal, e.g., 0755):</label>
          <input
            v-model="chmodDialog.mode"
            type="text"
            placeholder="0644"
            class="w-full px-3 py-2 rounded border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 font-mono"
            maxlength="4"
          />
        </div>

        <div v-if="chmodDialog.isDangerous" class="mb-4 p-3 bg-amber-50 dark:bg-amber-900/30 border border-amber-200 dark:border-amber-700 rounded">
          <p class="text-sm text-amber-800 dark:text-amber-200 font-medium">Warning: Dangerous Path</p>
          <p class="text-xs text-amber-700 dark:text-amber-300 mt-1">This path is in a system directory. Changing permissions may cause system issues.</p>
          <label class="flex items-center gap-2 mt-2 text-sm text-amber-800 dark:text-amber-200">
            <input type="checkbox" v-model="chmodDialog.forceConfirmed" class="rounded border-amber-400" />
            I understand the risks and want to proceed
          </label>
        </div>

        <div class="flex justify-end gap-2">
          <button
            type="button"
            class="px-4 py-2 text-sm rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700"
            @click="closeChmodDialog"
          >
            Cancel
          </button>
          <button
            type="button"
            class="px-4 py-2 text-sm rounded bg-indigo-600 hover:bg-indigo-700 text-white disabled:opacity-50"
            :disabled="!chmodDialog.mode || (chmodDialog.isDangerous && !chmodDialog.forceConfirmed)"
            @click="confirmChmod"
          >
            Apply
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue';
import { send, on as onWS } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, default: '' },
  mode: { type: String, default: 'local' }, // 'local' | 'remote'
  type: { type: String, default: 'source' }, // 'source' | 'destination'
  host: { type: String, default: '' },
  user: { type: String, default: '' },
  port: { type: Number, default: 22 },
  protocol: { type: String, default: '' }, // remote-only: 'ssh' | 'smb'
  share: { type: String, default: '' }, // smb-only
  profile: { type: String, default: '' }, // smb-only (agent config profile name)
  modelValue: { type: [String, Array], default: '' },
  title: { type: String, default: '' },
  enableFileOps: { type: Boolean, default: false } // Enable upload/delete/chmod actions
});

const emit = defineEmits(['update:modelValue']);

const isMulti = computed(() => props.type === 'source');
const title = computed(() => props.title || (isMulti.value ? 'Source directories' : 'Destination'));

const currentPath = ref('/');
const entries = ref([]);
const loading = ref(false);
const error = ref('');
const manual = ref(false);
const manualPath = ref('');

const lastRequestId = ref('');

const hostTrimmed = computed(() => String(props.host || '').trim());
const remoteProtocol = computed(() => {
  const p = String(props.protocol || '').trim().toLowerCase();
  return p === 'smb' ? 'smb' : 'ssh';
});
const shareTrimmed = computed(() => String(props.share || '').trim());
const profileTrimmed = computed(() => String(props.profile || '').trim());
const canBrowse = computed(() => {
  if (!props.clientId) return false;
  if (props.mode === 'remote') {
    if (!hostTrimmed.value) return false;
    if (remoteProtocol.value === 'smb' && (!shareTrimmed.value || !profileTrimmed.value)) return false;
  }
  return true;
});

const validation = ref('idle'); // idle | validating | ok | error
const validationBadgeText = computed(() => {
  if (props.mode !== 'remote') return '';
  if (!hostTrimmed.value) return 'host required';
  if (remoteProtocol.value === 'smb' && !shareTrimmed.value) return 'share required';
  if (remoteProtocol.value === 'smb' && !profileTrimmed.value) return 'profile required';
  if (validation.value === 'validating') return 'validating…';
  if (validation.value === 'ok') return 'connected';
  if (validation.value === 'error') return 'error';
  return 'idle';
});
const validationBadgeClass = computed(() => {
  const base = 'text-gray-600 dark:text-gray-200 border-gray-300 dark:border-gray-600';
  if (validation.value === 'ok') return 'text-green-700 dark:text-green-200 border-green-300 dark:border-green-700 bg-green-50 dark:bg-green-900/20';
  if (validation.value === 'error') return 'text-red-700 dark:text-red-200 border-red-300 dark:border-red-700 bg-red-50 dark:bg-red-900/20';
  if (validation.value === 'validating') return base + ' bg-gray-50 dark:bg-gray-800/30';
  return base;
});

function normalizePosixPath(p) {
  const s = String(p || '').trim();
  if (!s) return '';
  // Minimal normalization for UI; agent enforces stricter rules.
  let out = s;
  if (!out.startsWith('/')) out = '/' + out;
  out = out.replace(/\0/g, '');
  out = out.replace(/\/+$/g, '');
  if (out === '') out = '/';
  return out === '' ? '/' : out;
}

const manualPathNormalized = computed(() => {
  const p = normalizePosixPath(manualPath.value);
  return p && p !== '/' ? p : (p === '/' ? '/' : '');
});

const selectedList = computed(() => {
  if (isMulti.value) {
    return Array.isArray(props.modelValue) ? props.modelValue : [];
  }
  const s = typeof props.modelValue === 'string' ? props.modelValue : '';
  return s ? [s] : [];
});

function emitSelected(next) {
  emit('update:modelValue', next);
}

function addSelected(p) {
  const normalized = normalizePosixPath(p);
  if (!normalized) return;
  if (isMulti.value) {
    const arr = Array.isArray(props.modelValue) ? [...props.modelValue] : [];
    if (!arr.includes(normalized)) arr.push(normalized);
    emitSelected(arr);
  } else {
    emitSelected(normalized);
  }
}

function removeSelected(p) {
  if (isMulti.value) {
    const arr = Array.isArray(props.modelValue) ? props.modelValue.filter(x => x !== p) : [];
    emitSelected(arr);
  } else {
    emitSelected('');
  }
}

function isSelectedDir(e) {
  if (e.type !== 'dir') return false;
  const full = joinPath(currentPath.value, e.name);
  if (isMulti.value) return selectedList.value.includes(full);
  return selectedList.value[0] === full;
}

function joinPath(base, name) {
  const b = normalizePosixPath(base);
  if (b === '/') return '/' + name;
  return b + '/' + name;
}

function parentPath(p) {
  const s = normalizePosixPath(p);
  if (s === '/' || !s) return '/';
  const parts = s.split('/').filter(Boolean);
  parts.pop();
  return '/' + parts.join('/');
}

const breadcrumb = computed(() => {
  const p = normalizePosixPath(currentPath.value);
  if (p === '/') return [{ label: '/', path: '/' }];
  const parts = p.split('/').filter(Boolean);
  const crumbs = [{ label: '/', path: '/' }];
  let acc = '';
  for (const seg of parts) {
    acc += '/' + seg;
    crumbs.push({ label: seg, path: acc });
  }
  return crumbs;
});

function makeRequestId() {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  } catch {}
  return String(Date.now()) + '-' + Math.random().toString(16).slice(2);
}

function fetchDir(p) {
  const next = normalizePosixPath(p);
  if (!canBrowse.value) return;
  error.value = '';
  entries.value = [];
  loading.value = true;
  if (props.mode === 'remote') validation.value = 'validating';

  const requestId = makeRequestId();
  lastRequestId.value = requestId;
  send({
    type: 'dir_list_request',
    clientId: props.clientId,
    requestId,
    path: next,
    mode: props.mode,
    host: props.mode === 'remote' ? props.host : '',
    user: props.mode === 'remote' ? props.user : '',
    port: props.mode === 'remote' ? props.port : 22,
    protocol: props.mode === 'remote' ? remoteProtocol.value : '',
    share: props.mode === 'remote' ? shareTrimmed.value : '',
    profile: props.mode === 'remote' ? profileTrimmed.value : '',
  });
}

function navigate(p) {
  currentPath.value = normalizePosixPath(p);
  fetchDir(currentPath.value);
}

function goUp() {
  navigate(parentPath(currentPath.value));
}

function goRoot() {
  navigate('/');
}

function onEntryClick(e) {
  if (e.type !== 'dir') return;
  const full = joinPath(currentPath.value, e.name);
  addSelected(full);
}

function onEntryDblClick(e) {
  if (e.type !== 'dir') return;
  navigate(joinPath(currentPath.value, e.name));
}

function selectCurrentFolder() {
  addSelected(currentPath.value);
}

function goManual() {
  if (!manualPathNormalized.value) return;
  navigate(manualPathNormalized.value);
}

function selectManual() {
  if (!manualPathNormalized.value) return;
  addSelected(manualPathNormalized.value);
}

function formatSize(bytes) {
  if (bytes == null || bytes === 0) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let size = bytes;
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024;
    i++;
  }
  return size.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

function formatModTime(modTime) {
  if (!modTime) return '-';
  try {
    const d = new Date(modTime);
    if (isNaN(d.getTime())) return '-';
    // Format as "Jan 15 14:30" or "Jan 15 2024" if older than 6 months
    const now = new Date();
    const sixMonthsAgo = new Date(now.getTime() - 180 * 24 * 60 * 60 * 1000);
    const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const month = months[d.getMonth()];
    const day = d.getDate();
    if (d < sixMonthsAgo) {
      return `${month} ${day} ${d.getFullYear()}`;
    }
    const hours = String(d.getHours()).padStart(2, '0');
    const mins = String(d.getMinutes()).padStart(2, '0');
    return `${month} ${day} ${hours}:${mins}`;
  } catch {
    return '-';
  }
}

function truncateLinkTarget(target) {
  if (!target) return '';
  if (target.length <= 20) return target;
  return target.slice(0, 17) + '...';
}

// ---- File operations ----
const fileInput = ref(null);
const uploading = ref(false);
const uploadProgress = ref('');
const fileOpMessage = ref('');
const fileOpMessageClass = ref('');

const deleteDialog = ref({
  show: false,
  path: '',
  entry: null,
  isDir: false,
  isDangerous: false,
  recursive: false,
  forceConfirmed: false,
});

const chmodDialog = ref({
  show: false,
  path: '',
  entry: null,
  mode: '',
  isDangerous: false,
  forceConfirmed: false,
});

// Dangerous path prefixes that require force confirmation
const dangerousPrefixes = ['/bin', '/sbin', '/usr', '/lib', '/lib32', '/lib64', '/boot', '/snap', '/var/lib', '/var/cache'];

function isDangerousPath(p) {
  const lower = p.toLowerCase();
  return dangerousPrefixes.some(prefix => lower.startsWith(prefix));
}

function openDeleteDialog(entry) {
  const fullPath = joinPath(currentPath.value, entry.name);
  deleteDialog.value = {
    show: true,
    path: fullPath,
    entry,
    isDir: entry.type === 'dir',
    isDangerous: isDangerousPath(fullPath),
    recursive: false,
    forceConfirmed: false,
  };
}

function closeDeleteDialog() {
  deleteDialog.value.show = false;
}

function confirmDelete() {
  const { path, isDangerous, forceConfirmed, recursive } = deleteDialog.value;
  const force = isDangerous && forceConfirmed;
  
  const requestId = makeRequestId();
  pendingFileOps.set(requestId, { type: 'delete', path });
  
  send({
    type: 'file_delete_request',
    clientId: props.clientId,
    requestId,
    path,
    force,
    recursive,
  });
  
  closeDeleteDialog();
  fileOpMessage.value = 'Deleting...';
  fileOpMessageClass.value = 'text-gray-500 dark:text-gray-400';
}

function openChmodDialog(entry) {
  const fullPath = joinPath(currentPath.value, entry.name);
  // Try to extract octal mode from the mode string
  let defaultMode = '0644';
  if (entry.mode) {
    // The mode string might be like "-rw-r--r--", convert to octal
    const modeNum = parseModeStringToOctal(entry.mode);
    if (modeNum) defaultMode = modeNum;
  }
  chmodDialog.value = {
    show: true,
    path: fullPath,
    entry,
    mode: defaultMode,
    isDangerous: isDangerousPath(fullPath),
    forceConfirmed: false,
  };
}

function closeChmodDialog() {
  chmodDialog.value.show = false;
}

function confirmChmod() {
  const { path, mode, isDangerous, forceConfirmed } = chmodDialog.value;
  const force = isDangerous && forceConfirmed;
  
  const requestId = makeRequestId();
  pendingFileOps.set(requestId, { type: 'chmod', path, mode });
  
  send({
    type: 'file_chmod_request',
    clientId: props.clientId,
    requestId,
    path,
    mode,
    force,
  });
  
  closeChmodDialog();
  fileOpMessage.value = 'Changing permissions...';
  fileOpMessageClass.value = 'text-gray-500 dark:text-gray-400';
}

function parseModeStringToOctal(modeStr) {
  // Convert mode string like "drwxr-xr-x" or "-rw-r--r--" to octal
  if (!modeStr || modeStr.length < 10) return null;
  // Skip the first character (file type)
  const perms = modeStr.slice(-9);
  let octal = 0;
  
  // Owner
  if (perms[0] === 'r') octal += 0o400;
  if (perms[1] === 'w') octal += 0o200;
  if (perms[2] === 'x' || perms[2] === 's') octal += 0o100;
  
  // Group
  if (perms[3] === 'r') octal += 0o040;
  if (perms[4] === 'w') octal += 0o020;
  if (perms[5] === 'x' || perms[5] === 's') octal += 0o010;
  
  // Other
  if (perms[6] === 'r') octal += 0o004;
  if (perms[7] === 'w') octal += 0o002;
  if (perms[8] === 'x' || perms[8] === 't') octal += 0o001;
  
  return '0' + octal.toString(8);
}

const pendingFileOps = new Map();
const CHUNK_SIZE = 64 * 1024; // 64KB chunks

async function handleFileSelect(event) {
  const files = event.target.files;
  if (!files || files.length === 0) return;
  
  uploading.value = true;
  fileOpMessage.value = '';
  
  let uploaded = 0;
  let failed = 0;
  
  for (const file of files) {
    try {
      await uploadFile(file);
      uploaded++;
    } catch (err) {
      failed++;
      console.error('Upload failed:', file.name, err);
    }
  }
  
  uploading.value = false;
  uploadProgress.value = '';
  
  if (failed > 0) {
    fileOpMessage.value = `Uploaded ${uploaded}, failed ${failed}`;
    fileOpMessageClass.value = 'text-amber-600 dark:text-amber-400';
  } else {
    fileOpMessage.value = `Uploaded ${uploaded} file(s)`;
    fileOpMessageClass.value = 'text-green-600 dark:text-green-400';
  }
  
  // Refresh directory listing
  setTimeout(() => fetchDir(currentPath.value), 500);
  
  // Clear file input
  event.target.value = '';
  
  // Clear message after a delay
  setTimeout(() => { fileOpMessage.value = ''; }, 5000);
}

async function uploadFile(file) {
  return new Promise((resolve, reject) => {
    const requestId = makeRequestId();
    const destPath = joinPath(currentPath.value, file.name);
    
    pendingFileOps.set(requestId, {
      type: 'put',
      file,
      path: destPath,
      resolve,
      reject,
    });
    
    uploadProgress.value = `${file.name} (0%)`;
    
    // Start upload
    send({
      type: 'file_put_start',
      clientId: props.clientId,
      requestId,
      path: destPath,
      size: file.size,
      mode: '0644',
      force: false,
      overwrite: true,
    });
    
    // Read file and send chunks
    const reader = new FileReader();
    let offset = 0;
    
    function readNextChunk() {
      const slice = file.slice(offset, offset + CHUNK_SIZE);
      reader.readAsArrayBuffer(slice);
    }
    
    reader.onload = (e) => {
      const chunk = e.target.result;
      const chunkData = Array.from(new Uint8Array(chunk));
      
      send({
        type: 'file_put_chunk',
        clientId: props.clientId,
        requestId,
        offset,
        data: chunkData,
      });
      
      offset += chunk.byteLength;
      const percent = Math.round((offset / file.size) * 100);
      uploadProgress.value = `${file.name} (${percent}%)`;
      
      if (offset < file.size) {
        readNextChunk();
      } else {
        // All chunks sent, finish upload
        send({
          type: 'file_put_finish',
          clientId: props.clientId,
          requestId,
          checksum: '', // Optional
        });
      }
    };
    
    reader.onerror = () => {
      pendingFileOps.delete(requestId);
      reject(new Error('Failed to read file'));
    };
    
    readNextChunk();
  });
}

let off;
let offFilePut;
let offFileDelete;
let offFileChmod;

onMounted(() => {
  off = onWS('dir_list_response', (m) => {
    if (!m) return;
    if (m.clientId !== props.clientId) return;
    if (m.requestId && m.requestId !== lastRequestId.value) return;
    if (props.mode && m.mode && m.mode !== props.mode) return;

    loading.value = false;
    const err = String(m.error || '').trim();
    error.value = err;
    if (props.mode === 'remote') validation.value = err ? 'error' : 'ok';

    const p = normalizePosixPath(m.path || currentPath.value);
    if (p) currentPath.value = p;
    entries.value = Array.isArray(m.entries) ? m.entries : [];
  });

  // File operation result handlers
  offFilePut = onWS('file_put_result', (m) => {
    if (!m || m.clientId !== props.clientId) return;
    const pending = pendingFileOps.get(m.requestId);
    if (!pending || pending.type !== 'put') return;
    pendingFileOps.delete(m.requestId);
    
    if (m.ok) {
      pending.resolve && pending.resolve();
    } else {
      pending.reject && pending.reject(new Error(m.error || 'Upload failed'));
    }
  });

  offFileDelete = onWS('file_delete_result', (m) => {
    if (!m || m.clientId !== props.clientId) return;
    const pending = pendingFileOps.get(m.requestId);
    if (!pending || pending.type !== 'delete') return;
    pendingFileOps.delete(m.requestId);
    
    if (m.ok) {
      fileOpMessage.value = 'Deleted successfully';
      fileOpMessageClass.value = 'text-green-600 dark:text-green-400';
      // Refresh directory listing
      fetchDir(currentPath.value);
    } else {
      fileOpMessage.value = `Delete failed: ${m.error}`;
      fileOpMessageClass.value = 'text-red-600 dark:text-red-400';
    }
    setTimeout(() => { fileOpMessage.value = ''; }, 5000);
  });

  offFileChmod = onWS('file_chmod_result', (m) => {
    if (!m || m.clientId !== props.clientId) return;
    const pending = pendingFileOps.get(m.requestId);
    if (!pending || pending.type !== 'chmod') return;
    pendingFileOps.delete(m.requestId);
    
    if (m.ok) {
      fileOpMessage.value = 'Permissions changed';
      fileOpMessageClass.value = 'text-green-600 dark:text-green-400';
      // Refresh directory listing
      fetchDir(currentPath.value);
    } else {
      fileOpMessage.value = `Chmod failed: ${m.error}`;
      fileOpMessageClass.value = 'text-red-600 dark:text-red-400';
    }
    setTimeout(() => { fileOpMessage.value = ''; }, 5000);
  });

  // Initial load
  if (canBrowse.value) {
    currentPath.value = '/';
    fetchDir('/');
  }
});

onUnmounted(() => {
  try { off && off(); } catch {}
  try { offFilePut && offFilePut(); } catch {}
  try { offFileDelete && offFileDelete(); } catch {}
  try { offFileChmod && offFileChmod(); } catch {}
});

let credsTimer = null;
// Re-validate / refetch on credential changes.
watch(
  () => [props.clientId, props.mode, hostTrimmed.value, props.user, props.port, remoteProtocol.value, shareTrimmed.value, profileTrimmed.value],
  () => {
    if (!canBrowse.value) {
      loading.value = false;
      entries.value = [];
      error.value = '';
      if (props.mode === 'remote') validation.value = hostTrimmed.value ? 'idle' : 'idle';
      return;
    }
    // Debounce: avoid spamming while typing.
    if (credsTimer) clearTimeout(credsTimer);
    credsTimer = setTimeout(() => {
      currentPath.value = '/';
      fetchDir('/');
    }, 350);
  },
  { deep: false }
);
</script>
