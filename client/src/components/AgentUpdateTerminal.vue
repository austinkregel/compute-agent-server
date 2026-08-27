<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <span class="text-xs text-gray-500 dark:text-gray-400 font-mono" v-if="clientId">{{ clientId }}</span>
      <button
        type="button"
        class="px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-600 text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
        @click="clear"
      >
        Clear
      </button>
    </div>
    <div class="bg-black rounded-md overflow-hidden" style="height: 240px;">
      <div ref="termContainer" class="h-full w-full font-hack"></div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';
import { on as onWS, agentUpdateHistory } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, default: '' }
});

const termContainer = ref(null);
let term;
let fit;
const offFns = [];

function formatLine(entry) {
  const ts = entry.ts ? new Date(entry.ts).toISOString() : new Date().toISOString();
  const status = entry.ok ? '\x1b[32mOK\x1b[0m' : '\x1b[31mFAIL\x1b[0m';
  const tag = entry.tag ? ` tag=${entry.tag}` : '';
  const repo = entry.repo ? ` repo=${entry.repo}` : '';
  const err = entry.ok ? '' : ` err=${entry.error || entry.detail || 'unknown'}`;
  return `[${ts}] ${status}${tag}${repo}${err}`;
}

function writeHistory() {
  if (!term) return;
  term.clear();
  term.writeln('\x1b[36mMost recent update results for this agent:\x1b[0m');
  if (!props.clientId) {
    term.writeln('\x1b[33mNo client selected.\x1b[0m');
    return;
  }
  const arr = agentUpdateHistory[props.clientId] || [];
  if (!arr.length) {
    term.writeln('\x1b[33mNo update results received yet.\x1b[0m');
    return;
  }
  for (const e of arr.slice(-50)) term.writeln(formatLine(e));
}

function clear() {
  if (!term) return;
  term.clear();
}

function fitNow() {
  try { fit?.fit(); } catch {}
}

onMounted(() => {
  term = new Terminal({
    fontSize: 13,
    convertEol: true,
    scrollback: 2000,
    disableStdin: true,
    theme: { background: '#000000', foreground: '#e5e5e5' }
  });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.open(termContainer.value);
  requestAnimationFrame(() => {
    fitNow();
    writeHistory();
  });

  offFns.push(onWS('agent_update_result', (msg) => {
    // sharedWS already stores it; just render incrementally if it matches current client.
    if (!term) return;
    if (!props.clientId) return;
    if (msg.clientId !== props.clientId) return;
    const entry = {
      ts: msg.ts,
      ok: !!msg.ok,
      tag: msg.tag || '',
      repo: msg.repo || '',
      error: msg.error || '',
      detail: msg.detail || ''
    };
    term.writeln(formatLine(entry));
  }));

  window.addEventListener('resize', fitNow);
});

watch(() => props.clientId, () => {
  // When switching clients, repaint from stored history.
  requestAnimationFrame(() => {
    fitNow();
    writeHistory();
  });
});

onUnmounted(() => {
  window.removeEventListener('resize', fitNow);
  offFns.forEach(fn => fn());
  try { term?.dispose(); } catch {}
  term = null;
  fit = null;
});
</script>




