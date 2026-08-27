<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';

import QuickActions from '../components/QuickActions.vue';
import TerminalPanel from '../components/TerminalPanel.vue';

const route = useRoute();
const clientId = computed(() => String(route.params.clientId || ''));
</script>

<template>
  <div>
    <QuickActions :selected-client="clientId" />
    <div class="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
      <TerminalPanel v-if="clientId" :active-client="clientId" />
    </div>
  </div>
</template>


