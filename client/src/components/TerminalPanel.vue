<template>
  <!-- Terminal panel integrated into page flow -->
  <div
    class="shadow-lg border border-gray-300 dark:border-gray-700 flex flex-col rounded-lg overflow-hidden"
    :class="sessionId ? 'bg-black' : 'bg-gray-50 dark:bg-gray-900'"
    :style="sessionId ? { height: panelHeight + 'px', minHeight: MIN_HEIGHT + 'px' } : {}"
  >
    <!-- Drag handle -->
    <div
      v-if="sessionId"
      class="w-full h-2 cursor-row-resize bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600 transition-colors"
      title="Drag to resize"
      @mousedown="beginResize"
    ></div>
    <!-- Header -->
    <div class="px-4 py-1.5 border-b border-gray-300 dark:border-gray-600 flex justify-between items-center bg-white dark:bg-gray-800 select-none">
      <div class="flex items-center space-x-3">
        <h2 class="text-sm font-medium text-gray-900 dark:text-gray-100">Interactive Shell</h2>
        <span v-if="sessionId" class="text-[10px] tracking-wide font-mono text-gray-500 dark:text-gray-400">Session: {{ sessionId }}</span>
      </div>
      <div class="flex items-center space-x-2">
        <button v-if="sessionId" @click="clearTerminal" type="button" class="inline-flex items-center px-2 py-1 border border-gray-300 dark:border-gray-600 shadow-sm text-[11px] font-medium rounded text-gray-700 dark:text-gray-200 bg-white dark:bg-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600 focus:outline-none">
          Clear
        </button>
      </div>
    </div>
    <!-- Terminal area -->
    <div v-if="sessionId" class="flex-1 bg-black overflow-hidden" ref="terminalWrapper">
      <div ref="termContainer" class="h-full w-full font-hack"></div>
    </div>
    <!-- Placeholder when no session -->
    <div v-else class="p-8 text-center text-gray-500 dark:text-gray-400">
      <p class="text-sm">No active shell session. Use the "Open Shell" action above to start one.</p>
    </div>
  </div>
</template>
<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue';
import { on as onWS, send } from '../lib/sharedWS.js';
import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';

const props = defineProps({
  activeClient: { type: String, default: '' }
});
const termContainer = ref(null);
const terminalWrapper = ref(null);
const sessionId = ref('');
const visible = ref(true); // Only visible while a session is active
// Resizable panel height
const DEFAULT_HEIGHT = 400;
const MIN_HEIGHT = 160;
const MAX_HEIGHT = 1200; // reasonable max for page flow
const panelHeight = ref(loadStoredHeight());
let resizing = false;
let resizeStartY = 0;
let resizeStartHeight = 0;
let term; let fitAddon; let resizeTimer; let rafId;
const offFns = [];

function calcMaxHeight() { 
  if (typeof window === 'undefined' || !window.innerHeight || window.innerHeight <= 0) {
    return MAX_HEIGHT; // Fallback to max if window not available
  }
  // Use a reasonable portion of viewport height for page flow
  return Math.min(MAX_HEIGHT, Math.floor(window.innerHeight * 0.7)); 
}
function clamp(v, a, b) { return Math.min(Math.max(v, a), b); }
function loadStoredHeight() {
  if (typeof localStorage === 'undefined') return DEFAULT_HEIGHT;
  const raw = localStorage.getItem('terminalPanel.height');
  const num = Number(raw);
  if (!raw || Number.isNaN(num)) return DEFAULT_HEIGHT;
  const maxH = calcMaxHeight();
  return clamp(num, MIN_HEIGHT, maxH);
}
function storeHeight(h) { try { localStorage.setItem('terminalPanel.height', String(h)); } catch {} }

function beginResize(e) {
  e.preventDefault();
  resizing = true;
  resizeStartY = e.clientY;
  resizeStartHeight = panelHeight.value;
  document.addEventListener('mousemove', onDragMove);
  document.addEventListener('mouseup', endResize, { once: true });
}
function onDragMove(e) {
  if (!resizing) return;
  const deltaY = resizeStartY - e.clientY; // Negative when dragging down (increase height)
  const maxH = calcMaxHeight();
  const newH = clamp(resizeStartHeight + deltaY, MIN_HEIGHT, maxH);
  panelHeight.value = newH;
  if (!rafId) {
    rafId = requestAnimationFrame(() => { rafId = null; scheduleResize(); });
  }
}
function endResize() {
  resizing = false;
  document.removeEventListener('mousemove', onDragMove);
  storeHeight(panelHeight.value);
  scheduleResize();
}

function initTerminal() {
  if (term) return console.log('Terminal already initialized');
  console.log('Initializing terminal');
  
  // Ensure panel has a valid height before opening terminal
  if (panelHeight.value < MIN_HEIGHT) {
    panelHeight.value = MIN_HEIGHT;
  }
  
  term = new Terminal({
    fontSize: 14,
    convertEol: true,
    scrollback: 2000,
    theme: {
      background: '#000000', foreground: '#e5e5e5', cursor: '#e5e5e5'
    }
  });
  fitAddon = new FitAddon();
  term.loadAddon(fitAddon);
  
  // Wait for DOM to update and container to have dimensions
  const tryOpenTerminal = () => {
    if (!termContainer.value || !term) return;
    
    // Ensure container is visible and has dimensions
    const container = termContainer.value;
    if (container.offsetWidth === 0 || container.offsetHeight === 0) {
      // Retry after a short delay if container still has no dimensions
      setTimeout(tryOpenTerminal, 100);
      return;
    }
    
    try {
      term.open(termContainer.value);
      if (fitAddon) {
        fitAddon.fit();
        // Wait a frame for renderer to initialize before writing
        requestAnimationFrame(() => {
          if (term) {
            try {
              term.writeln('\x1b[32mInteractive shell ready. Use the Open Shell action to start a session.\x1b[0m');
              fitAddon.fit();
              } catch (err) {
              console.error('[TerminalPanel] Failed to write initial message - terminal renderer not ready:', {
                error: err,
                message: err?.message,
                stack: err?.stack,
                containerDimensions: container ? { width: container.offsetWidth, height: container.offsetHeight } : null
              });
            }
          }
        });
      }
    } catch (err) {
      console.error('[TerminalPanel] Failed to open terminal:', {
        error: err,
        message: err?.message,
        stack: err?.stack,
        containerDimensions: container ? { width: container.offsetWidth, height: container.offsetHeight } : null,
        panelHeight: panelHeight.value
      });
    }
  };
  
  requestAnimationFrame(tryOpenTerminal);
  
  term.onData(data => {
    if (!sessionId.value) return; // ignore until session
    if (data === '\r') data = '\n';
    send({ type: 'shell_input', session: sessionId.value, data });
  });
}

function sendResize() {
  if (!sessionId.value || !term) return;
  // Validate terminal dimensions before sending - invalid dimensions can cause shell to close
  const cols = term.cols || 0;
  const rows = term.rows || 0;
  if (cols <= 0 || rows <= 0) {
    console.warn('[TerminalPanel] Cannot send resize - invalid dimensions:', { cols, rows, sessionId: sessionId.value });
    return;
  }
  send({ type: 'shell_resize', session: sessionId.value, cols, rows });
}

function scheduleResize() {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(()=>{ 
    if (fitAddon && term) { 
      try {
        fitAddon.fit();
        sendResize(); 
      } catch (err) {
        console.error('[TerminalPanel] Failed to schedule resize:', {
          error: err,
          message: err?.message,
          terminalReady: !!term,
          fitAddonReady: !!fitAddon
        });
      }
    } 
  }, 150);
}

function clearTerminal() {
  if (term) {
    term.clear();
  }
}

// Visibility bound to session lifecycle

function handleShellStarted(msg) {
  console.log('[TerminalPanel] Shell started', { session: msg.session, clientId: msg.clientId });
  sessionId.value = msg.session;
  // Set reasonable default height if not already set
  if (!panelHeight.value || panelHeight.value < MIN_HEIGHT) {
    panelHeight.value = DEFAULT_HEIGHT;
  }
  storeHeight(panelHeight.value);
  
  // Initialize terminal after ensuring panel is visible
  initTerminal();
  
  // Schedule resize after terminal is initialized
  // Wait longer to ensure terminal renderer is fully ready before sending resize
  setTimeout(() => {
    if (term && fitAddon) {
      try {
        // Ensure terminal is fitted before getting dimensions
        fitAddon.fit();
        // Wait another frame for dimensions to be calculated
        requestAnimationFrame(() => {
          if (term && sessionId.value) {
            const cols = term.cols || 0;
            const rows = term.rows || 0;
            console.log('[TerminalPanel] Sending initial resize', { 
              session: sessionId.value, 
              cols, 
              rows,
              valid: cols > 0 && rows > 0
            });
            sendResize();
          } else {
            console.warn('[TerminalPanel] Cannot send initial resize - terminal not ready', {
              hasTerm: !!term,
              hasSession: !!sessionId.value
            });
          }
        });
      } catch (err) {
        console.error('[TerminalPanel] Failed to send initial resize:', {
          error: err,
          message: err?.message,
          stack: err?.stack,
          sessionId: sessionId.value,
          terminalCols: term?.cols,
          terminalRows: term?.rows
        });
      }
    } else {
      console.warn('[TerminalPanel] Cannot send initial resize - terminal not initialized', {
        hasTerm: !!term,
        hasFitAddon: !!fitAddon,
        sessionId: sessionId.value
      });
    }
  }, 300); // Increased delay to ensure terminal is fully initialized
}
function handleShellOutput(msg) { 
  if (msg.data && sessionId.value === msg.session && term) {
    try {
      term.write(msg.data);
    } catch (err) {
      // Terminal renderer might not be ready yet - log error but don't crash
      console.error('[TerminalPanel] Failed to write shell output - renderer not ready:', {
        error: err,
        message: err?.message,
        stack: err?.stack,
        dataLength: msg.data?.length,
        sessionId: sessionId.value,
        terminalState: term ? 'exists' : 'null'
      });
    }
  }
}
function handleShellClosed(msg) {
  if (sessionId.value === msg.session) {
    console.log('[TerminalPanel] Shell closed', { 
      session: msg.session, 
      reason: msg.reason, 
      code: msg.code,
      signal: msg.signal
    });
    if (term) {
      try {
        term.writeln(`\x1b[33mShell closed: ${msg.reason || 'done'}\x1b[0m`);
      } catch (err) {
        // Terminal might already be disposed, but log it for debugging
        console.warn('[TerminalPanel] Could not write shell close message:', {
          error: err,
          message: err?.message,
          reason: msg.reason,
          sessionId: sessionId.value
        });
      }
    }
    try {
      if (term) {
        term.dispose();
      }
      // Keep the stored height for next session, but clear session
      term = null;
      fitAddon = null;
    } catch (err) {
      console.error('[TerminalPanel] Failed to dispose terminal:', {
        error: err,
        message: err?.message,
        stack: err?.stack,
        sessionId: sessionId.value
      });
      term = null;
      fitAddon = null;
    }
    sessionId.value='';
  }
}

onMounted(() => {
  visible.value = !!sessionId.value;
  window.addEventListener('resize', scheduleResize);
  window.addEventListener('resize', handleWindowConstrain);
  offFns.push(onWS('shell_started', handleShellStarted));
  offFns.push(onWS('shell_output', handleShellOutput));
  offFns.push(onWS('shell_closed', handleShellClosed));
  offFns.push(onWS('shell_error', (msg) => {
    if (sessionId.value === msg.session) {
      if (term) {
        try {
          term.writeln(`\n\x1b[31m[Shell error: ${msg.error}]\x1b[0m`);
        } catch (err) {
          // Terminal might not be ready - log the error
          console.error('[TerminalPanel] Failed to write shell error message:', {
            error: err,
            message: err?.message,
            stack: err?.stack,
            shellError: msg.error,
            sessionId: sessionId.value
          });
        }
      }
      sessionId.value = '';
    }
  }));
  // Auto toggle visibility based on session presence
  watch(sessionId, (val) => {
    const has = !!val;
    if (has && !visible.value) {
      visible.value = true;
      setTimeout(() => scheduleResize(), 40);
    } else if (!has && visible.value) {
      visible.value = false;
    }
  }, { immediate: true });
});
onUnmounted(() => {
  window.removeEventListener('resize', scheduleResize);
  window.removeEventListener('resize', handleWindowConstrain);
  offFns.forEach(fn=>fn());
  document.removeEventListener('mousemove', onDragMove);
  try { term?.dispose(); } catch {}
});

function handleWindowConstrain() {
  const maxH = calcMaxHeight();
  if (maxH > 0 && panelHeight.value > maxH) {
    panelHeight.value = maxH;
    storeHeight(panelHeight.value);
  }
}
</script>