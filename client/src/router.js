import { createRouter, createWebHistory } from 'vue-router';
import { checkAuth, isAdmin } from './lib/auth.js';

import DashboardView from './views/DashboardView.vue';
import ExecAllowlistView from './views/ExecAllowlistView.vue';
import AuditView from './views/AuditView.vue';
import ActionsView from './views/ActionsView.vue';
import LogsView from './views/LogsView.vue';
import BackupsView from './views/BackupsView.vue';
import SmsView from './views/SmsView.vue';
import KioskView from './views/KioskView.vue';
import FleetView from './views/FleetView.vue';
import DockerView from './views/DockerView.vue';
import StacksView from './views/StacksView.vue';
import StackDetailView from './views/StackDetailView.vue';
import EnvGroupsView from './views/EnvGroupsView.vue';
import ListOfConnectedClients from './components/ListOfConnectedClients.vue';
import LoginPage from './components/LoginPage.vue';

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { 
      path: '/login', 
      component: LoginPage,
      meta: { requiresAuth: false }
    },
    { 
      path: '/', 
      component: ListOfConnectedClients,
      meta: { requiresAuth: true }
    },
    { 
      path: '/fleet', 
      component: FleetView,
      meta: { requiresAuth: true }
    },
    { 
      path: '/stacks', 
      component: StacksView,
      meta: { requiresAuth: true }
    },
    { 
      path: '/stacks/:stackId', 
      component: StackDetailView,
      meta: { requiresAuth: true }
    },
    {
      path: '/env-groups',
      component: EnvGroupsView,
      meta: { requiresAuth: true }
    },
    {
      path: '/admin/exec-allowlist',
      component: ExecAllowlistView,
      meta: { requiresAuth: true, requiresAdmin: true }
    },
    {
      path: '/admin/audit',
      component: AuditView,
      meta: { requiresAuth: true, requiresAdmin: true }
    },
    { 
      path: '/client/:clientId', 
      component: DashboardView,
      meta: { requiresAuth: true }
    },
    {
      path: '/client/:clientId/backups',
      component: BackupsView,
      meta: { requiresAuth: true }
    },
    {
      // Admin-gated to match the server-side REST enforcement (see
      // api/routes.go's admin-gated group) — SMS content is as sensitive as
      // exec/restart, not a general-purpose per-client tab.
      path: '/client/:clientId/sms',
      component: SmsView,
      meta: { requiresAuth: true, requiresAdmin: true }
    },
    { 
      path: '/client/:clientId/actions', 
      component: ActionsView,
      meta: { requiresAuth: true }
    },
    { 
      path: '/client/:clientId/kiosk', 
      component: KioskView,
      meta: { requiresAuth: true }
    },
    { 
      path: '/client/:clientId/docker', 
      component: DockerView,
      meta: { requiresAuth: true }
    },
    { 
      path: '/client/:clientId/logs', 
      component: LogsView,
      meta: { requiresAuth: true }
    },
    // Catch-all: avoid blank screens for bad URLs like `/actions` (or `/`-typos).
    // Redirect to home; auth guard still applies.
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
      meta: { requiresAuth: true }
    }
  ],
});

// Navigation guard to check authentication
// OIDC is now REQUIRED - always redirect to login if not authenticated
router.beforeEach(async (to, from, next) => {
  // Skip auth check for login page
  if (to.path === '/login') {
    return next();
  }

  // Check if route requires authentication
  if (to.meta.requiresAuth !== false) {
    const authenticated = await checkAuth();
    if (!authenticated) {
      // Store intended destination for redirect after login
      const redirect = to.fullPath !== '/' ? to.fullPath : undefined;
      return next({ path: '/login', query: redirect ? { redirect } : {} });
    }
    // Admin-only routes: checkAuth populated isAdmin from the server's gate.
    // Non-admins are bounced home rather than shown a route they can't use.
    if (to.meta.requiresAdmin && !isAdmin.value) {
      return next({ path: '/' });
    }
  }

  next();
});

export default router;



