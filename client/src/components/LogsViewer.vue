<script setup>
import { ref, onMounted, onUnmounted, watch, nextTick, computed } from 'vue';
import { on as onWS, send, connected, logTailState } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, required: true },
});

const linesRequested = 10;
const session = ref('');
const running = computed(() => !!(props.clientId && session.value && logTailState[props.clientId]?.running));
const buffer = ref('');
const scroller = ref(null);
const autoScroll = ref(true);

function scrollToBottom() {
  const el = scroller.value;
  if (!el) return;
  el.scrollTop = el.scrollHeight;
}

function maybeAutoScroll() {
  if (!autoScroll.value) return;
  nextTick(() => scrollToBottom());
}

function clear() {
  buffer.value = '';
}

function start() {
  if (!props.clientId) return;
  send({ type: 'log_tail_start', clientId: props.clientId, lines: linesRequested });
}

function stop() {
  if (!props.clientId) return;
  if (session.value) {
    send({ type: 'log_tail_stop', session: session.value, clientId: props.clientId });
  } else {
    send({ type: 'log_tail_stop', clientId: props.clientId });
  }
}

const off = [];
onMounted(() => {
  off.push(onWS('log_tail_started', (msg) => {
    if (msg.clientId !== props.clientId) return;
    session.value = msg.session || '';
  }));
  off.push(onWS('log_tail_output', (msg) => {
    if (msg.clientId !== props.clientId) return;
    if (session.value && msg.session && msg.session !== session.value) return;
    if (typeof msg.data === 'string' && msg.data.length) {
      buffer.value += msg.data;
      // Keep memory bounded (roughly last ~200KB).
      if (buffer.value.length > 200_000) buffer.value = buffer.value.slice(-200_000);
      maybeAutoScroll();
    }
  }));
  off.push(onWS('log_tail_closed', (msg) => {
    if (msg.clientId !== props.clientId) return;
    if (session.value && msg.session && msg.session !== session.value) return;
  }));
  off.push(onWS('log_tail_error', (msg) => {
    if (msg.clientId && msg.clientId !== props.clientId) return;
    const m = msg.message || msg.error || 'log tail error';
    buffer.value += `\n[error] ${m}\n`;
    maybeAutoScroll();
  }));

  start();
});

onUnmounted(() => {
  try { stop(); } catch {}
  off.forEach(fn => fn());
});

watch(() => props.clientId, (cid, prev) => {
  if (!cid) return;
  if (prev && prev !== cid) {
    session.value = '';
    buffer.value = '';
    start();
  }
});

function onScroll() {
  const el = scroller.value;
  if (!el) return;
  // If user scrolls up, disable autoscroll. If they return near bottom, re-enable.
  const nearBottom = (el.scrollHeight - (el.scrollTop + el.clientHeight)) < 32;
  autoScroll.value = nearBottom;
}
</script>

<template>
  <section class="bg-white dark:bg-gray-800 shadow rounded-lg border border-transparent dark:border-gray-700 overflow-hidden">
    <header class="px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
      <div class="flex items-center gap-3">
        <h3 class="text-sm font-semibold text-gray-700 dark:text-gray-200">Agent Logs (live tail)</h3>
        <span class="text-[10px] font-mono text-gray-500 dark:text-gray-400">last {{ linesRequested }} lines</span>
        <span class="text-[10px] px-2 py-0.5 rounded border"
          :class="connected ? 'border-green-300 text-green-700 bg-green-50 dark:border-green-600 dark:text-green-200 dark:bg-green-900/30'
                           : 'border-red-300 text-red-700 bg-red-50 dark:border-red-600 dark:text-red-200 dark:bg-red-900/30'">
          {{ connected ? 'connected' : 'disconnected' }}
        </span>
        <span v-if="session" class="text-[10px] font-mono text-gray-500 dark:text-gray-400">session: {{ session }}</span>
      </div>
      <div class="flex items-center gap-2">
        <button type="button" class="inline-flex items-center px-2 py-1 border border-gray-300 dark:border-gray-600 shadow-sm text-[11px] font-medium rounded text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600"
          @click="clear">
          Clear
        </button>
        <button v-if="!running" type="button" class="inline-flex items-center px-2 py-1 border border-gray-300 dark:border-gray-600 shadow-sm text-[11px] font-medium rounded text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600"
          @click="start">
          Start
        </button>
        <button v-else type="button" class="inline-flex items-center px-2 py-1 border border-gray-300 dark:border-gray-600 shadow-sm text-[11px] font-medium rounded text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600"
          @click="stop">
          Stop
        </button>
      </div>
    </header>

    <div class="p-0 bg-black">
      <div ref="scroller" class="h-[50vh] overflow-auto" @scroll="onScroll">
        <pre class="text-[12px] leading-5 text-gray-100 p-4 whitespace-pre-wrap break-words font-mono">{{ buffer || 'Waiting for logs…' }}</pre>
      </div>
    </div>
  </section>
</template>














