<script setup>
import Sparkline from './Sparkline.vue';
const props = defineProps({
  data: { type: Array, default: () => [] },
  width: { type: Number, default: 80 },
  height: { type: Number, default: 24 },
  label: { type: String, default: 'CPU load history' }
});
</script>
<template>
  <div class="inline-flex items-center justify-center border border-yellow-500/30 dark:border-yellow-400/30 rounded-md bg-yellow-50/50 dark:bg-yellow-950/20 p-1">
    <Sparkline :data="props.data" :width="props.width" :height="props.height" :label="props.label" cls="text-yellow-500 dark:text-yellow-400" />
  </div>
</template>
