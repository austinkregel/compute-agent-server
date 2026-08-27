<script setup>
import Sparkline from './Sparkline.vue';
const props = defineProps({
  data: { type: Array, default: () => [] },
  width: { type: Number, default: 80 },
  height: { type: Number, default: 24 },
  label: { type: String, default: 'Memory usage history' },
  min: { type: Number, default: 0 },
  max: { type: Number, default: 100 }
});
</script>
<template>
  <div class="inline-flex items-center justify-center border border-red-500/30 dark:border-red-400/30 rounded-md bg-red-50/50 dark:bg-red-950/20 p-1">
    <Sparkline :data="props.data" :width="props.width" :height="props.height" :label="props.label" :min="props.min" :max="props.max" cls="text-red-500 dark:text-red-400" />
  </div>
</template>
