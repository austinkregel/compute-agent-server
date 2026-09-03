<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import {
  smsThreadsMap, smsMessagesMap, smsErrorMap,
  fetchSmsThreads, fetchSmsMessages, sendSms,
  on as onWS,
} from '../lib/sharedWS.js';

const route = useRoute();
const clientId = computed(() => String(route.params.clientId || ''));

const threads = computed(() => smsThreadsMap[clientId.value] || []);
const loadError = computed(() => smsErrorMap[clientId.value] || '');
const activeThreadId = ref(null);
const activeThread = computed(() => threads.value.find(t => String(t.threadId) === String(activeThreadId.value)) || null);
const messages = computed(() => smsMessagesMap[`${clientId.value}:${activeThreadId.value}`] || []);

const composeTo = ref('');
const composeBody = ref('');
const sending = ref(false);
const sendError = ref('');
const showNewConversation = ref(false);

async function loadThreads() {
  const list = await fetchSmsThreads(clientId.value);
  if (!activeThreadId.value && list.length > 0) {
    activeThreadId.value = list[0].threadId;
  }
}

function selectThread(threadId) {
  activeThreadId.value = threadId;
  showNewConversation.value = false;
}

watch(activeThreadId, async (threadId) => {
  if (threadId) {
    await fetchSmsMessages(clientId.value, threadId);
  }
});

// Select the newest conversation whenever one shows up and nothing is open.
// loadThreads only auto-selects at mount, so the very first message to arrive
// on a phone with no prior history landed in the sidebar while the
// conversation pane still read "Select a conversation" — which looked like the
// message had not arrived at all.
watch(threads, (list) => {
  if (!activeThreadId.value && !showNewConversation.value && list.length > 0) {
    activeThreadId.value = list[0].threadId;
  }
});

let unsubscribe;
onMounted(async () => {
  await loadThreads();
  unsubscribe = onWS('sms_received', (obj) => {
    if (obj.clientId !== clientId.value) return;
    // The push doesn't carry a threadId, so refresh whatever's open — cheap
    // for SMS volumes, and keeps this simple (see sharedWS.js's own comment
    // on the same tradeoff for the thread-list refresh). When nothing is open
    // yet, the threads watcher above picks up the new conversation instead.
    if (activeThreadId.value) {
      fetchSmsMessages(clientId.value, activeThreadId.value);
    }
  });
});
onUnmounted(() => { if (unsubscribe) unsubscribe(); });

async function handleSend() {
  const to = activeThread.value ? activeThread.value.address : composeTo.value.trim();
  const body = composeBody.value.trim();
  if (!to || !body) return;

  sending.value = true;
  sendError.value = '';
  try {
    const result = await sendSms(clientId.value, to, body);
    if (result.error) {
      sendError.value = result.error;
      return;
    }
    composeBody.value = '';
    await loadThreads();
    if (result.threadId) {
      selectThread(result.threadId);
    } else if (activeThread.value) {
      await fetchSmsMessages(clientId.value, activeThreadId.value);
    }
    if (!activeThread.value) {
      showNewConversation.value = false;
      composeTo.value = '';
    }
  } finally {
    sending.value = false;
  }
}

function formatTs(ts) {
  if (!ts) return '';
  const d = new Date(ts);
  return isNaN(d) ? '' : d.toLocaleString();
}
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4 h-[70vh]">
      <!-- Thread list -->
      <div class="md:col-span-1 rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 flex flex-col overflow-hidden">
        <div class="px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex items-center justify-between">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Conversations</h2>
          <button
            class="text-xs px-2 py-1 rounded bg-blue-600 text-white hover:bg-blue-700"
            @click="activeThreadId = null; showNewConversation = true"
          >
            New
          </button>
        </div>
        <div class="flex-1 overflow-y-auto divide-y divide-gray-100 dark:divide-gray-700">
          <div v-if="loadError" class="p-4 text-sm text-red-600 dark:text-red-400">
            {{ loadError }}
          </div>
          <div v-else-if="threads.length === 0" class="p-4 text-sm text-gray-500 dark:text-gray-400">
            No SMS history yet.
          </div>
          <button
            v-for="t in threads"
            :key="t.threadId"
            class="w-full text-left px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-700/50"
            :class="String(t.threadId) === String(activeThreadId) ? 'bg-blue-50 dark:bg-blue-900/20' : ''"
            @click="selectThread(t.threadId)"
          >
            <div class="flex items-center justify-between">
              <span class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t.displayName || t.address }}</span>
              <span v-if="t.unreadCount > 0" class="text-xs px-1.5 py-0.5 rounded-full bg-blue-600 text-white">{{ t.unreadCount }}</span>
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400 truncate">{{ t.snippet }}</div>
          </button>
        </div>
      </div>

      <!-- Conversation + compose -->
      <div class="md:col-span-2 rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 flex flex-col overflow-hidden">
        <div v-if="!activeThread && !showNewConversation" class="flex-1 flex items-center justify-center text-sm text-gray-500 dark:text-gray-400">
          Select a conversation or start a new one.
        </div>

        <template v-else>
          <div class="px-4 py-3 border-b border-gray-200 dark:border-gray-700">
            <template v-if="activeThread">
              <span class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ activeThread.displayName || activeThread.address }}</span>
            </template>
            <template v-else>
              <input
                v-model="composeTo"
                type="text"
                placeholder="Phone number"
                class="w-full text-sm px-2 py-1 rounded border border-gray-300 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100"
              />
            </template>
          </div>

          <div class="flex-1 overflow-y-auto p-4 space-y-2">
            <div
              v-for="m in messages"
              :key="m.messageId"
              class="max-w-[75%] rounded-lg px-3 py-2 text-sm"
              :class="m.direction === 'out'
                ? 'ml-auto bg-blue-600 text-white'
                : 'bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100'"
            >
              <div>{{ m.body }}</div>
              <div class="text-[10px] opacity-70 mt-1">{{ formatTs(m.timestamp) }}</div>
            </div>
          </div>

          <div class="p-3 border-t border-gray-200 dark:border-gray-700">
            <div v-if="sendError" class="text-xs text-red-600 dark:text-red-400 mb-2">{{ sendError }}</div>
            <form class="flex gap-2" @submit.prevent="handleSend">
              <input
                v-model="composeBody"
                type="text"
                placeholder="Type a message..."
                class="flex-1 text-sm px-3 py-2 rounded border border-gray-300 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100"
              />
              <button
                type="submit"
                :disabled="sending || !composeBody.trim() || (!activeThread && !composeTo.trim())"
                class="px-4 py-2 rounded bg-blue-600 text-white text-sm hover:bg-blue-700 disabled:opacity-50"
              >
                {{ sending ? 'Sending…' : 'Send' }}
              </button>
            </form>
          </div>
        </template>
      </div>
    </div>
  </main>
</template>
