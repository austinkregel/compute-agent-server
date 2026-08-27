<template>
  <svg :width="width" :height="height" :viewBox="`0 0 ${width} ${height}`" fill="none" stroke="currentColor" :class="cls" role="img" :aria-label="ariaLabel">
    <!-- If we have 2+ points, draw the polyline -->
    <polyline v-if="hasLine" :points="points" stroke-width="1.5" stroke="currentColor" fill="none" stroke-linejoin="round" stroke-linecap="round" vector-effect="non-scaling-stroke" />
    <!-- If only a single point, draw a small circle so something is visible -->
    <circle v-else-if="isSinglePoint" :cx="singlePointXY.x" :cy="singlePointXY.y" r="2" fill="currentColor" />
    <title>{{ ariaLabel }}</title>
  </svg>
</template>
<script setup>
import { computed } from 'vue';

const props = defineProps({
  data: { type: Array, default: () => [] },
  width: { type: Number, default: 80 },
  height: { type: Number, default: 24 },
  cls: { type: String, default: '' },
  min: { type: Number, default: null },
  max: { type: Number, default: null },
  label: { type: String, default: 'sparkline' }
});

// Normalize numeric data array; if refs slipped in accidentally, Vue template unwrapping should handle.
const dataArray = computed(() => Array.isArray(props.data) ? props.data : []);

const statsBounds = computed(() => {
  const xs = dataArray.value;
  if (!xs.length) return { min: 0, max: 1 };
  const minVal = props.min ?? Math.min(...xs);
  const maxVal = props.max ?? Math.max(...xs);
  return { min: minVal, max: maxVal === minVal ? minVal + 1 : maxVal };
});

const isSinglePoint = computed(() => dataArray.value.length === 1);
const hasLine = computed(() => dataArray.value.length >= 2);

const points = computed(() => {
  const xs = dataArray.value;
  if (xs.length < 2) return '';
  const w = props.width;
  const h = props.height;
  const { min, max } = statsBounds.value;
  const span = (max - min) || 1;
  return xs.map((v, i) => {
    const x = (i / (xs.length - 1)) * (w - 2) + 1; // padding 1px
    const norm = (v - min) / span;
    const y = h - 1 - norm * (h - 2);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');
});

const singlePointXY = computed(() => {
  if (!isSinglePoint.value) return { x: 0, y: 0 };
  const v = dataArray.value[0];
  const w = props.width; const h = props.height;
  const { min, max } = statsBounds.value; const span = (max - min) || 1;
  const norm = (v - min) / span;
  const y = h - 1 - norm * (h - 2);
  return { x: w / 2, y };
});

const ariaLabel = computed(() => props.label + (props.data.length ? ` (last=${props.data[props.data.length-1].toFixed?.(2) ?? props.data[props.data.length-1]})` : ' (no data)'));
</script>
<style scoped>
svg { display: block; }
</style>
