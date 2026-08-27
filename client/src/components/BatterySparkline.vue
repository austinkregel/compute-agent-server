<script setup>
import Sparkline from './Sparkline.vue';
const props = defineProps({
  data: { type: Array, default: () => [] },
  width: { type: Number, default: 80 },
  height: { type: Number, default: 24 },
  label: { type: String, default: 'Battery level history' },
  min: { type: Number, default: 0 },
  max: { type: Number, default: 100 }
});
</script>
<template>
  <div class="inline-flex items-center justify-center border border-green-500/30 dark:border-green-400/30 rounded-md bg-green-50/50 dark:bg-green-950/20 p-1">
    <Sparkline :data="props.data" :width="props.width" :height="props.height" :label="props.label" :min="props.min" :max="props.max" cls="text-green-500 dark:text-green-400" />
  </div>
</template>
