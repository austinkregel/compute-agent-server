<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-900 py-12 px-4 sm:px-6 lg:px-8">
    <div class="max-w-md w-full space-y-8">
      <div>
        <h2 class="mt-6 text-center text-3xl font-extrabold text-gray-900 dark:text-gray-100 dark:bg-gray-900">
          Sign in to Dashboard
        </h2>
        <p class="mt-2 text-center text-sm text-gray-600 dark:text-gray-400 dark:bg-gray-900">
          Please authenticate to access the backup server dashboard
        </p>
      </div>
      <div class="mt-8 space-y-6">
        <button
          @click="handleLogin"
          class="group relative w-full flex justify-center py-2 px-4 border border-transparent text-sm font-medium rounded-md text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
        >
          Sign in with OIDC
        </button>
        <div v-if="error" class="rounded-md bg-red-50 p-4">
          <div class="flex">
            <div class="ml-3">
              <h3 class="text-sm font-medium text-red-800">
                Authentication Error
              </h3>
              <div class="mt-2 text-sm text-red-700">
                <p>{{ error }}</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { checkAuth, login } from '../lib/auth.js';

const router = useRouter();
const error = ref(null);

async function handleLogin() {
  error.value = null;
  try {
    // Check if already authenticated
    const authenticated = await checkAuth();
    if (authenticated) {
      // Redirect to intended destination or home
      const redirect = router.currentRoute.value.query.redirect || '/';
      router.push(redirect);
      return;
    }
    // Redirect to login
    login();
  } catch (err) {
    error.value = err.message || 'Failed to initiate login';
  }
}

onMounted(async () => {
  // Check if already authenticated
  const authenticated = await checkAuth();
  if (authenticated) {
    // Redirect to intended destination or home
    const redirect = router.currentRoute.value.query.redirect || '/';
    router.push(redirect);
  }
});
</script>









