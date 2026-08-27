<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between">
      <p class="text-xs text-indigo-600 dark:text-indigo-400 font-medium">{{ cronScopeHint }}</p>
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ cronStats }}</span>
    </div>
    <div class="flex items-center gap-2">
      <button @click="reload" type="button" class="px-3 py-1.5 text-xs rounded-md bg-gray-200 hover:bg-gray-300 text-gray-800 dark:bg-gray-700 dark:hover:bg-gray-600 dark:text-gray-100 transition-colors" :disabled="loading">{{ loading? 'Loading...':'Reload' }}</button>
      <button @click="validate" type="button" class="px-3 py-1.5 text-xs rounded-md bg-indigo-600 hover:bg-indigo-700 text-white disabled:opacity-50 transition-colors" :disabled="loading">Validate</button>
      <button @click="save" type="button" class="px-3 py-1.5 text-xs rounded-md bg-green-600 hover:bg-green-700 text-white disabled:opacity-50 transition-colors" :disabled="loading || !dirty">Save</button>
    </div>
    <textarea v-model="content" @input="onInput" rows="10" class="w-full font-mono text-xs rounded-md border border-gray-300 dark:border-gray-600 focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500 dark:bg-gray-900 dark:text-gray-100 p-3" placeholder="# Example: Run backup at 3:05 daily
5 3 * * * /usr/local/bin/backup-script >> /var/log/backup.log 2>&1"></textarea>
    <ul v-if="errors.length" class="space-y-1">
      <li v-for="e in errors" :key="e.line" class="text-xs text-red-600 dark:text-red-400">Line {{ e.line }}: {{ e.error }}</li>
    </ul>
    <div v-if="message" :class="['p-2 rounded-md text-xs',
      messageType==='success'
        ? 'text-green-700 dark:text-green-300'
        : messageType==='error'
          ? 'text-red-700 dark:text-red-300'
          : 'text-gray-600 dark:text-gray-300'
    ]">{{ message }}</div>
  </div>
</template>
<script setup>
import { ref, watch, computed } from 'vue';

// Props: allow parent to specify scope (local/server or a client)
const props = defineProps({
  scopeClient: { type: String, default: '' }
});
const emit = defineEmits(['saved','validated','error']);

const cronStats = ref('');
const cronScopeHint = ref('');
const content = ref('');
const original = ref('');
const loading = ref(false);
const errors = ref([]); // {line,error}
const message = ref('');
const messageType = ref('info');
const dirty = computed(()=> content.value !== original.value);

watch(()=>props.scopeClient, ()=>{
  cronScopeHint.value = props.scopeClient ? `Viewing remote crontab for ${props.scopeClient}` : 'Editing server-local crontab';
  reload();
});

function setMessage(msg, type='info'){ message.value = msg; messageType.value=type; setTimeout(()=>{ if(message.value===msg) message.value=''; }, 5000); }

function updateStats(){
  const lines = content.value.split(/\n/).filter(l=> l.trim() && !l.trim().startsWith('#')).length;
  cronStats.value = `${lines} active entries`;
}

async function reload(){
  loading.value = true; errors.value=[]; setMessage('');
  try {
    const url = props.scopeClient ? `/api/client/${encodeURIComponent(props.scopeClient)}/cron` : '/api/cron';
    const res = await fetch(url);
    if(!res.ok) throw new Error('Failed to load crontab');
    const data = await res.json();
    content.value = data.crontab || '';
    original.value = content.value;
    updateStats();
  } catch (e) {
    setMessage('Could not load crontab','error');
    emit('error', e);
  } finally { loading.value=false; }
}

async function validate(){
  loading.value = true; errors.value=[]; setMessage('');
  try {
    const res = await fetch('/api/cron/validate', { method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ crontab: content.value }) });
    const data = await res.json();
    if(!res.ok){ errors.value = data.errors || []; setMessage('Validation failed','error'); emit('error', data); }
    else { setMessage('Crontab valid','success'); emit('validated'); }
  } catch (e) {
    setMessage('Validation error','error'); emit('error', e);
  } finally { loading.value=false; }
}

async function save(){
  loading.value = true; setMessage('');
  try {
    const res = await fetch('/api/cron', { method:'PUT', headers:{'Content-Type':'application/json'}, body: JSON.stringify({ crontab: content.value }) });
    const data = await res.json();
    if(!res.ok){ errors.value = data.details || []; setMessage('Save failed','error'); emit('error', data); }
    else { original.value = content.value; updateStats(); setMessage('Crontab updated','success'); emit('saved'); }
  } catch (e) {
    setMessage('Save error','error'); emit('error', e);
  } finally { loading.value=false; }
}

function onInput(){ updateStats(); }

// initial load
reload();
</script>
