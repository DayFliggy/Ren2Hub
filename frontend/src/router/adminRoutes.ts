import type { RouteRecordRaw } from 'vue-router'

export const adminRoutes: RouteRecordRaw[] = [
  {
    path: '/task-plugins',
    name: 'task-plugins',
    component: () => import('@/views/console/TaskPluginsView.vue'),
    meta: {
      requiresAdmin: true,
      requiresRoot: true,
      feature: 'taskPlugins',
      protected: true,
    },
  },
  {
    path: '/billing/reconciliations',
    name: 'billing-reconciliations',
    component: () => import('@/views/admin/BillingReconciliationsView.vue'),
    meta: {
      requiresAdmin: true,
      requiresRoot: true,
      feature: 'billing_reconciliation',
      protected: true,
    },
  },
  {
    path: '/models/metadata',
    name: 'models-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    meta: { requiresAdmin: true, feature: 'admin', protected: true },
  },
  {
    path: '/models/vendors',
    name: 'vendors-admin',
    redirect: (to) => ({
      path: '/models/metadata',
      query: { ...to.query, manage: 'vendors' },
      hash: to.hash,
    }),
  },
  {
    path: '/models/prefill-groups',
    name: 'prefill-admin',
    redirect: (to) => ({
      path: '/models/metadata',
      query: { ...to.query, manage: 'prefill-groups' },
      hash: to.hash,
    }),
  },
  // The single-instance gateway has no instance list; its monitor lives at /ops.
  {
    path: '/system-info',
    name: 'system-info',
    redirect: { name: 'ops-monitor' },
  },
]
