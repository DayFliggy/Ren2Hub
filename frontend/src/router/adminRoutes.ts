import type { RouteRecordRaw } from 'vue-router'

export const adminRoutes: RouteRecordRaw[] = [
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
  {
    path: '/system-info',
    name: 'system-info',
    component: () => import('@/views/admin/SystemManagementView.vue'),
    meta: {
      requiresAdmin: true,
      requiresRoot: true,
      feature: 'admin',
      protected: true,
    },
  },
]
