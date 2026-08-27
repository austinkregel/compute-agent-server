<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';

import BackupWidget from '../components/BackupWidget.vue';

const route = useRoute();
const clientId = computed(() => String(route.params.clientId || ''));
</script>

<template>
  <main class="max-w-7xl mx-auto py-6 px-4 sm:px-6 lg:px-8">
    <BackupWidget :client-id="clientId" />
  </main>
</template>
