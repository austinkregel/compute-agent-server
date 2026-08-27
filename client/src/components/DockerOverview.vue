<script setup>
import { computed, ref } from 'vue';
import { dockerStatusMap, clientHasCapability, send, on } from '../lib/sharedWS.js';

const props = defineProps({
  clientId: { type: String, required: true }
});

const dockerStatus = computed(() => dockerStatusMap[props.clientId] || null);
// Additive migration to the generic capability registry: prefer the
// capability signal, but fall back to the old ad hoc presence check until
// every agent in the field reports capabilities.
const hasDocker = computed(() => clientHasCapability(props.clientId, 'docker') || !!dockerStatus.value?.available);
const swarm = computed(() => dockerStatus.value?.swarm || null);
const isSwarmActive = computed(() => swarm.value?.localNodeState === 'active');
const isManager = computed(() => swarm.value?.controlAvailable);

const containers = computed(() => dockerStatus.value?.containers || {});
const runningCount = computed(() => containers.value.running || 0);
const stoppedCount = computed(() => containers.value.stopped || 0);
const totalCount = computed(() => containers.value.total || 0);

const showSwarmInit = ref(false);
const showSwarmJoin = ref(false);
const swarmInitAddr = ref('');
const swarmJoinToken = ref('');
const swarmJoinAddrs = ref('');
const swarmJoinAdvAddr = ref('');
const swarmActionPending = ref(false);
const swarmActionResult = ref(null);

function initSwarm() {
  swarmActionPending.value = true;
  swarmActionResult.value = null;
  send({
    type: 'swarm_init_request',
    clientId: props.clientId,
    advertiseAddr: swarmInitAddr.value || undefined
  });
}

function joinSwarm() {
  swarmActionPending.value = true;
  swarmActionResult.value = null;
  const addrs = swarmJoinAddrs.value.split(',').map(s => s.trim()).filter(Boolean);
  send({
    type: 'swarm_join_request',
    clientId: props.clientId,
    joinToken: swarmJoinToken.value,
    remoteAddrs: addrs,
    advertiseAddr: swarmJoinAdvAddr.value || undefined
  });
}

function leaveSwarm() {
  if (!confirm('Leave the Docker Swarm? This will remove this node from the cluster.')) return;
  swarmActionPending.value = true;
  swarmActionResult.value = null;
  send({
    type: 'swarm_leave_request',
    clientId: props.clientId,
    force: true
  });
}

on('swarm_init_result', (msg) => {
  if (msg.clientId !== props.clientId) return;
  swarmActionPending.value = false;
  swarmActionResult.value = msg;
  if (msg.ok) showSwarmInit.value = false;
});

on('swarm_join_result', (msg) => {
  if (msg.clientId !== props.clientId) return;
  swarmActionPending.value = false;
  swarmActionResult.value = msg;
  if (msg.ok) showSwarmJoin.value = false;
});

on('swarm_leave_result', (msg) => {
  if (msg.clientId !== props.clientId) return;
  swarmActionPending.value = false;
  swarmActionResult.value = msg;
});
</script>

<template>
  <div class="rounded-lg bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 p-5">
    <div class="flex items-center justify-between mb-4">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Docker Overview</h3>
      <span v-if="hasDocker" class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300">
        <span class="w-1.5 h-1.5 rounded-full bg-green-500 mr-1"></span>
        v{{ dockerStatus.version || '?' }}
      </span>
      <span v-else class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400">
        Unavailable
      </span>
    </div>

    <template v-if="hasDocker">
      <div class="grid grid-cols-3 gap-3 mb-4">
        <div class="rounded-lg bg-gray-50 dark:bg-gray-700/50 p-3 text-center">
          <div class="text-lg font-bold text-gray-900 dark:text-gray-100">{{ totalCount }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400">Total</div>
        </div>
        <div class="rounded-lg bg-gray-50 dark:bg-gray-700/50 p-3 text-center">
          <div class="text-lg font-bold text-green-600 dark:text-green-400">{{ runningCount }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400">Running</div>
        </div>
        <div class="rounded-lg bg-gray-50 dark:bg-gray-700/50 p-3 text-center">
          <div class="text-lg font-bold text-gray-500 dark:text-gray-400">{{ stoppedCount }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400">Stopped</div>
        </div>
      </div>

      <div class="border-t border-gray-200 dark:border-gray-700 pt-3">
        <div class="flex items-center justify-between">
          <div class="text-xs text-gray-500 dark:text-gray-400">
            <span class="font-medium">Swarm:</span>
            <span v-if="isSwarmActive" class="ml-1">
              {{ isManager ? 'Manager' : 'Worker' }}
              <span v-if="swarm.clusterId" class="text-gray-400 dark:text-gray-500 ml-1">
                ({{ swarm.clusterId?.slice(0, 12) }})
              </span>
            </span>
            <span v-else class="ml-1 text-gray-400">Inactive</span>
          </div>
          <div class="flex gap-1.5">
            <template v-if="!isSwarmActive">
              <button @click="showSwarmInit = true" class="px-2 py-1 text-xs rounded bg-blue-600 text-white hover:bg-blue-700 transition-colors">Init</button>
              <button @click="showSwarmJoin = true" class="px-2 py-1 text-xs rounded bg-gray-600 text-white hover:bg-gray-700 transition-colors">Join</button>
            </template>
            <button v-else @click="leaveSwarm" class="px-2 py-1 text-xs rounded bg-red-600 text-white hover:bg-red-700 transition-colors">Leave</button>
          </div>
        </div>

        <div v-if="isSwarmActive && swarm" class="mt-2 text-xs text-gray-500 dark:text-gray-400 space-y-0.5">
          <div v-if="swarm.nodeAddr">Node: {{ swarm.nodeAddr }}</div>
          <div v-if="swarm.managers != null">Managers: {{ swarm.managers }} &middot; Nodes: {{ swarm.nodes }}</div>
        </div>

        <div v-if="swarmActionResult" class="mt-2 p-2 rounded text-xs" :class="swarmActionResult.ok ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300'">
          {{ swarmActionResult.ok ? 'Success' : (swarmActionResult.error || 'Failed') }}
        </div>
      </div>

      <!-- Swarm Init Modal -->
      <Teleport to="body">
        <div v-if="showSwarmInit" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showSwarmInit = false">
          <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md shadow-xl">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Initialize Swarm</h4>
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">Advertise Address (optional)</label>
            <input v-model="swarmInitAddr" class="w-full px-3 py-2 text-sm border rounded dark:bg-gray-700 dark:border-gray-600 dark:text-gray-100 mb-3" placeholder="e.g. 192.168.1.10:2377" />
            <div class="flex justify-end gap-2">
              <button @click="showSwarmInit = false" class="px-3 py-1.5 text-xs rounded bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300">Cancel</button>
              <button @click="initSwarm" :disabled="swarmActionPending" class="px-3 py-1.5 text-xs rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50">
                {{ swarmActionPending ? 'Initializing...' : 'Initialize' }}
              </button>
            </div>
          </div>
        </div>
      </Teleport>

      <!-- Swarm Join Modal -->
      <Teleport to="body">
        <div v-if="showSwarmJoin" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50" @click.self="showSwarmJoin = false">
          <div class="bg-white dark:bg-gray-800 rounded-lg p-6 w-full max-w-md shadow-xl">
            <h4 class="text-sm font-semibold text-gray-900 dark:text-gray-100 mb-3">Join Swarm</h4>
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">Join Token</label>
            <input v-model="swarmJoinToken" class="w-full px-3 py-2 text-sm border rounded dark:bg-gray-700 dark:border-gray-600 dark:text-gray-100 mb-2" placeholder="SWMTKN-..." />
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">Remote Addresses (comma-separated)</label>
            <input v-model="swarmJoinAddrs" class="w-full px-3 py-2 text-sm border rounded dark:bg-gray-700 dark:border-gray-600 dark:text-gray-100 mb-2" placeholder="192.168.1.10:2377" />
            <label class="block text-xs text-gray-500 dark:text-gray-400 mb-1">Advertise Address (optional)</label>
            <input v-model="swarmJoinAdvAddr" class="w-full px-3 py-2 text-sm border rounded dark:bg-gray-700 dark:border-gray-600 dark:text-gray-100 mb-3" placeholder="e.g. 192.168.1.20:2377" />
            <div class="flex justify-end gap-2">
              <button @click="showSwarmJoin = false" class="px-3 py-1.5 text-xs rounded bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300">Cancel</button>
              <button @click="joinSwarm" :disabled="swarmActionPending || !swarmJoinToken || !swarmJoinAddrs" class="px-3 py-1.5 text-xs rounded bg-blue-600 text-white hover:bg-blue-700 disabled:opacity-50">
                {{ swarmActionPending ? 'Joining...' : 'Join' }}
              </button>
            </div>
          </div>
        </div>
      </Teleport>
    </template>

    <div v-else class="text-center py-4 text-sm text-gray-500 dark:text-gray-400">
      Docker is not available on this node.
    </div>
  </div>
</template>
