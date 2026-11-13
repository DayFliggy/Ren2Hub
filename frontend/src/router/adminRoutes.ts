import type { RouteRecordRaw } from 'vue-router'

export const adminRoutes: RouteRecordRaw[] = [
  {
    path: '/system-settings/request-policies/filtering',
    redirect: '/request-processing',
  },
  {
    path: '/system-settings/security/sensitive',
    redirect: '/request-processing',
  },
  {
    path: '/system-settings/security/sensitive-words',
    redirect: '/request-processing',
  },
  {
    path: '/request-processing',
    name: 'request-processing',
    component: () => import('@/features/task-plugins/RequestProcessing.vue'),
    meta: {
      requiresAdmin: true,
      requiresRoot: true,
      feature: 'taskPlugins',
      requiresPermission: { resource: 'plugin', action: 'write' },
      protected: true,
    },
  },
  {
    path: '/oauth/plugin/:key/callback',
    name: 'plugin-login-callback',
    component: () => import('@/features/channels/PluginLoginCallback.vue'),
    meta: {
      requiresAdmin: true,
      requiresPermission: { resource: 'channel', action: 'sensitive_write' },
      feature: 'admin',
      protected: true,
    },
  },
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
  // All sections share the wide admin layout used by the other admin lists.
  {
    path: '/models/metadata',
    name: 'models-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'metadata' },
    meta: {
      wide: true,
      requiresAdmin: true,
      feature: 'admin',
      protected: true,
    },
  },
  {
    path: '/models/vendors',
    name: 'vendors-admin',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'vendors' },
    meta: {
      wide: true,
      requiresAdmin: true,
      feature: 'admin',
      protected: true,
      nav: 'models-admin',
    },
  },
  {
    path: '/models/pricing',
    name: 'models-pricing',
    component: () => import('@/views/admin/ModelManagementView.vue'),
    props: { section: 'pricing' },
    meta: {
      wide: true,
      requiresAdmin: true,
      requiresRoot: true,
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
