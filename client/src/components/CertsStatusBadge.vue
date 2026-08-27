
<template>
  <span v-if="status === 'ok'"
    class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-800 dark:text-blue-200">
    <ShieldCheckIcon class="w-4 h-4 mr-1 text-blue-500" />
    Certs Synced
  </span>
  <span v-else-if="status === 'outdated'"
    class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-yellow-100 text-yellow-800 dark:bg-yellow-800 dark:text-yellow-200">
    <ShieldExclamationIcon class="w-4 h-4 mr-1 text-yellow-500" />
    Certs Outdated
  </span>
  <span v-else
    class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-800 dark:text-gray-200">
    <ShieldExclamationIcon class="w-4 h-4 mr-1 text-gray-400" />
    Certs Unknown
  </span>
</template>
<script setup>
import { ShieldCheckIcon, ShieldExclamationIcon } from '@heroicons/vue/24/solid';
const props = defineProps({
  status: { type: String, default: 'unknown' }
});
</script>
