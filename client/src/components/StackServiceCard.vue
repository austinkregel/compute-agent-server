<script setup>
import { computed } from 'vue';

const props = defineProps({
  service: { type: Object, required: true },
  stackName: { type: String, default: '' },
});

const isPublic = computed(() => props.service.ports?.some(p => p.published));

const publishedPorts = computed(() =>
  (props.service.ports || []).filter(p => p.published)
);

const internalPorts = computed(() =>
  (props.service.ports || []).filter(p => !p.published)
);

const labels = computed(() => {
  const l = props.service.labels || {};
  return Object.entries(l).slice(0, 5);
});
</script>

<template>
  <div
    class="rounded-lg bg-white dark:bg-gray-800 p-4 border"
    :class="isPublic
      ? 'border-blue-200 dark:border-blue-800'
      : 'border-gray-200 dark:border-gray-700'"
  >
    <div class="flex items-center justify-between mb-2">
      <div class="flex items-center gap-2">
        <span class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ service.name }}</span>
        <span v-if="isPublic" class="px-1.5 py-0.5 rounded text-[10px] font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300">Public</span>
      </div>
    </div>

    <div class="space-y-1.5 text-xs text-gray-500 dark:text-gray-400">
      <div class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Image</span>
        <span class="font-mono truncate text-gray-700 dark:text-gray-300">{{ service.image }}</span>
      </div>

      <div v-if="publishedPorts.length" class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Ports</span>
        <div class="flex flex-wrap gap-1">
          <span
            v-for="(p, i) in publishedPorts"
            :key="'pub-' + i"
            class="px-1.5 py-0.5 rounded bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-300 font-mono"
          >{{ p.published }}:{{ p.target }}/{{ p.protocol || 'tcp' }}</span>
          <span
            v-for="(p, i) in internalPorts"
            :key="'int-' + i"
            class="px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 font-mono"
          >{{ p.target }}/{{ p.protocol || 'tcp' }}</span>
        </div>
      </div>

      <div v-if="service.volumes?.length" class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Volumes</span>
        <div class="flex flex-col gap-0.5">
          <span v-for="(v, i) in service.volumes" :key="i" class="font-mono truncate">
            {{ typeof v === 'string' ? v : `${v.source}:${v.target}` }}
          </span>
        </div>
      </div>

      <div v-if="service.healthcheck" class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Health</span>
        <span class="font-mono truncate">{{ Array.isArray(service.healthcheck.test) ? service.healthcheck.test.join(' ') : service.healthcheck.test }}</span>
      </div>

      <div v-if="service.depends_on?.length || service.dependsOn?.length" class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Deps</span>
        <span>{{ (service.depends_on || service.dependsOn || []).join(', ') }}</span>
      </div>

      <div v-if="service.deploy?.resources" class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Limits</span>
        <span class="font-mono">
          <template v-if="service.deploy.resources.limits?.cpus">CPU: {{ service.deploy.resources.limits.cpus }}</template>
          <template v-if="service.deploy.resources.limits?.memory"> Mem: {{ service.deploy.resources.limits.memory }}</template>
        </span>
      </div>

      <div v-if="labels.length" class="flex gap-2">
        <span class="text-gray-400 dark:text-gray-500 w-16 flex-shrink-0">Labels</span>
        <div class="flex flex-wrap gap-1">
          <span
            v-for="([key, val], i) in labels"
            :key="i"
            class="px-1 py-0.5 rounded bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-400 font-mono truncate max-w-[200px]"
          >{{ key }}={{ val }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
