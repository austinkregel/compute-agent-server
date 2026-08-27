<template>
  <div class="relative inline-block text-left w-64">
    <Listbox v-model="selected">
      <div class="relative">
        <ListboxButton class="inline-flex justify-between w-full rounded-md border border-gray-300 dark:border-gray-600 shadow-sm px-3 py-1.5 bg-white dark:bg-gray-800 text-sm font-medium text-gray-700 dark:text-gray-200 hover:bg-gray-50 dark:hover:bg-gray-700 focus:outline-none">
          <span>{{ selectedLabel }}</span>
          <svg class="-mr-1 ml-2 h-4 w-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"/></svg>
        </ListboxButton>
        <ListboxOptions class="absolute z-50 mt-1 w-full bg-white dark:bg-gray-800 shadow-lg max-h-60 rounded-md py-1 text-base ring-1 ring-black ring-opacity-5 overflow-auto focus:outline-none border border-gray-200 dark:border-gray-600">
          <!-- All Clients link -->
          <ListboxOption value="" class="cursor-pointer px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700" :class="{'font-semibold': !selected}">
            All Clients
          </ListboxOption>
          <!-- Client links -->
          <ListboxOption v-for="c in clients" :key="c.clientId" :value="c.clientId" class="cursor-pointer px-4 py-2 text-sm hover:bg-gray-100 dark:hover:bg-gray-700" :class="{'font-semibold': selected === c.clientId}">
            {{ c.clientId }}
          </ListboxOption>
        </ListboxOptions>
      </div>
    </Listbox>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { Listbox, ListboxButton, ListboxOptions, ListboxOption } from '@headlessui/vue';

const props = defineProps({
  clients: Array,
  modelValue: { type: String, default: '' }
});
const emit = defineEmits(['update:modelValue']);
const router = useRouter();
const route = useRoute();
const selected = ref(String(props.modelValue || ''));
const selectedLabel = computed(() => selected.value || 'All Clients');

watch(selected, async (v) => {
  emit('update:modelValue', v);
  // Navigate when selection changes
  // If switching to "All Clients", always go to root.
  // Otherwise we risk producing paths like `//actions` when replacing `/client/:id` with `/`.
  if (!v) {
    await router.push('/');
    return;
  }

  const base = `/client/${encodeURIComponent(v)}`;
  const onClientRoute = String(route.path || '').startsWith('/client/');
  const target = onClientRoute ? route.path.replace(/\/client\/[^/]+/, base) : base;
  await router.push(target);
});
watch(() => props.modelValue, v => { if (v !== selected.value) selected.value = v; });
</script>
