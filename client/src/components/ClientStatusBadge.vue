<script setup>
import { computed, ref, onMounted, onUnmounted } from 'vue';
const props = defineProps({
  client: {
    type: Object,
    required: true
  },
  variant: {
    type: String,
    default: 'badge' // 'badge' | 'dot'
  },
});

// Compute online status from last stats ts (300s / 5 min threshold) + tooltip
const ONLINE_THRESHOLD_MS = 300000;
const now = ref(Date.now());
let _timer;
onMounted(() => { _timer = setInterval(() => { now.value = Date.now(); }, 5000); });
onUnmounted(() => { if (_timer) clearInterval(_timer); });

const lastTimestampMs = computed(() => {
  const val = props.client?.ts;
  if (typeof val === 'number') return val;
  if (typeof val === 'string') {
    const ms = Date.parse(val);
    return isNaN(ms) ? 0 : ms;
  }
  return 0;
});
const online = computed(() => lastTimestampMs.value && (now.value - lastTimestampMs.value < ONLINE_THRESHOLD_MS));
const lastSeenSeconds = computed(() => lastTimestampMs.value ? Math.floor((now.value - lastTimestampMs.value)/1000) : null);
const tooltip = computed(() => {
  if (!lastTimestampMs.value) return 'No data received yet';
  return `Last seen ${lastSeenSeconds.value}s ago`;
});
</script>

<template>
  <span
    v-if="props.variant === 'dot'"
    :title="tooltip"
    class="inline-flex items-center"
    :aria-label="online ? 'Online' : 'Offline'"
  >
    <span
      class="inline-block w-2.5 h-2.5 rounded-full ring-2 ring-white/10"
      :class="online ? 'bg-green-500' : 'bg-red-500'"
    />
  </span>

  <span
    v-else-if="online"
    :title="tooltip"
    class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-800 dark:bg-green-800 dark:text-green-200"
  >
    Online
  </span>
  <span
    v-else
    :title="tooltip"
    class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-red-100 text-red-800 dark:bg-red-800 dark:text-red-200"
  >
    Offline
  </span>
</template>
