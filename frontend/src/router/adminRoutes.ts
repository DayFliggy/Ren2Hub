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
  // NewAPI model management: "Models" and "Vendors" sections of one page.
  {
    path: '/models/metadata',
    name: 'models-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'metadata' },
    meta: { requiresAdmin: true, feature: 'admin', protected: true },
  },
  {
    path: '/models/vendors',
    name: 'vendors-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'vendors' },
    meta: {
      requiresAdmin: true,
      feature: 'admin',
      protected: true,
      nav: 'models-admin',
    },
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
