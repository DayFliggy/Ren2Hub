import type { RouteRecordRaw } from 'vue-router'

export const adminRoutes: RouteRecordRaw[] = [
  {
    path: 'admin/models',
    name: 'models-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'models' },
    meta: { requiresAdmin: true },
  },
  {
    path: 'admin/vendors',
    name: 'vendors-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'vendors' },
    meta: { requiresAdmin: true },
  },
  {
    path: 'admin/prefill-groups',
    name: 'prefill-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'groups' },
    meta: { requiresAdmin: true },
  },
  {
    path: 'admin/deployments',
    name: 'deployments-admin',
    component: () => import('@/views/admin/DeploymentsView.vue'),
    meta: { requiresAdmin: true },
  },
  {
    path: 'admin/system-info',
    name: 'system-info',
    component: () => import('@/views/admin/SystemManagementView.vue'),
    meta: { requiresAdmin: true, requiresRoot: true },
  },
]
