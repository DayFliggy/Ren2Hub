import type { RouteRecordRaw } from 'vue-router'

export const adminRoutes: RouteRecordRaw[] = [
  {
    path: '/models/metadata',
    name: 'models-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'models' },
    meta: { requiresAdmin: true, feature: 'admin', protected: true },
  },
  {
    path: '/models/vendors',
    name: 'vendors-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'vendors' },
    meta: { requiresAdmin: true, feature: 'admin', protected: true },
  },
  {
    path: '/models/prefill-groups',
    name: 'prefill-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'groups' },
    meta: { requiresAdmin: true, feature: 'admin', protected: true },
  },
  {
    path: '/models/deployments',
    name: 'deployments-admin',
    component: () => import('@/views/admin/DeploymentsView.vue'),
    meta: { requiresAdmin: true, feature: 'admin', protected: true },
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
