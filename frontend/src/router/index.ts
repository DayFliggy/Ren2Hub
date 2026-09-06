import {
  createRouter,
  createWebHistory,
  type RouteLocationRaw,
} from 'vue-router'

import HomeView from '@/views/HomeView.vue'
import { getConsoleRouteAccessMeta } from '@/constants/navigation/consoleNav'
import { loadMessageDomain } from '@/i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useSetupStore } from '@/stores/setup'
import { publicRoutes } from '@/router/publicRoutes'
import { adminRoutes } from '@/router/adminRoutes'
import { canonicalLegacyPath } from '@/router/legacyRoutes'
import { navigationError, navigationPending } from '@/router/navigationState'

const CONSOLE_ENTRY: RouteLocationRaw = { name: 'dashboard' }
const CHUNK_RELOAD_KEY = 'ren2hub_chunk_reload'

export function sanitizeSetupRedirect(value: unknown): string | null {
  if (
    typeof value !== 'string' ||
    !value.startsWith('/') ||
    value.startsWith('//')
  ) {
    return null
  }
  try {
    const url = new URL(value, window.location.origin)
    if (url.origin !== window.location.origin) return null
    const pathname = canonicalLegacyPath(url.pathname, url.searchParams)
    if (pathname === '/setup/error') return null
    return `${pathname}${url.search}${url.hash}`
  } catch {
    return null
  }
}

export function sanitizeRedirect(value: unknown): string | null {
  if (
    typeof value !== 'string' ||
    !value.startsWith('/') ||
    value.startsWith('//')
  ) {
    return null
  }

  try {
    const url = new URL(value, window.location.origin)
    if (url.origin !== window.location.origin) return null
    const pathname = canonicalLegacyPath(url.pathname, url.searchParams)
    const target = router.resolve(pathname)
    if (
      !target.matched.length ||
      target.name === 'not-found' ||
      target.meta.guestOnly ||
      target.meta.setupRoute ||
      target.meta.setupError
    )
      return null
    return `${pathname}${url.search}${url.hash}`
  } catch {
    return null
  }
}

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: HomeView,
      meta: { public: true },
    },
    { path: '/home', redirect: { name: 'home' } },
    {
      path: '/sign-in',
      name: 'sign-in',
      component: () => import('@/views/auth/SignInView.vue'),
      meta: { public: true, guestOnly: true, messageDomain: 'auth' },
    },
    {
      path: '/sign-up',
      name: 'sign-up',
      component: () => import('@/views/auth/SignUpView.vue'),
      meta: {
        public: true,
        guestOnly: true,
        feature: 'registration',
        messageDomain: 'auth',
      },
    },
    {
      path: '/forgot-password',
      name: 'reset',
      component: () => import('@/views/auth/ResetPasswordView.vue'),
      meta: { public: true, guestOnly: true, messageDomain: 'auth' },
    },
    {
      path: '/reset',
      name: 'reset-confirm',
      component: () => import('@/views/auth/ResetPasswordConfirmView.vue'),
      meta: { public: true, messageDomain: 'auth' },
    },
    {
      path: '/oauth/:provider',
      name: 'oauth-callback',
      component: () => import('@/views/auth/OAuthCallbackView.vue'),
      meta: { public: true, messageDomain: 'auth' },
    },
    {
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/setup/SetupView.vue'),
      meta: { public: true, setupRoute: true },
    },
    {
      path: '/setup/error',
      name: 'setup-error',
      component: () => import('@/views/setup/SetupErrorView.vue'),
      meta: { public: true, setupError: true },
    },
    ...publicRoutes,
    {
      path: '/console',
      component: () => import('@/components/layout/ConsoleLayout.vue'),
      meta: { requiresAuth: true, topNav: 'console', messageDomain: 'console' },
      children: [
        { path: '', redirect: { name: 'dashboard' } },
        ...adminRoutes,
        {
          path: '/dashboard',
          name: 'dashboard',
          component: () => import('@/views/console/DashboardView.vue'),
          meta: { topNav: 'dashboard', feature: 'dashboard_basic' },
        },
        {
          path: '/activity',
          name: 'activity',
          component: () => import('@/views/console/ActivityView.vue'),
          meta: {
            topNav: 'activities',
            feature: 'activity',
          },
        },
        {
          path: '/models',
          name: 'models',
          component: () => import('@/views/console/ModelsView.vue'),
          meta: { feature: 'user_models' },
        },
        {
          path: '/market',
          name: 'market',
          component: () => import('@/views/console/MarketplaceView.vue'),
          meta: {
            noPageScroll: true,
            protected: true,
            feature: 'marketplace',
          },
        },
        {
          path: '/keys',
          name: 'keys',
          component: () => import('@/views/console/KeysView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'legacy_token',
          },
        },
        {
          path: '/keys/:id/routing',
          name: 'token-routing',
          component: () => import('@/views/console/TokenRoutingView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            protected: true,
            feature: 'token_private_routing',
            capability: 'token_private_routing',
          },
        },
        {
          path: '/usage-logs',
          name: 'logs',
          component: () => import('@/views/console/LogsView.vue'),
          meta: { wide: true, noPageScroll: true, feature: 'logs' },
        },
        {
          path: '/usage-logs/drawing',
          name: 'logs-drawing',
          component: () => import('@/views/console/DrawingLogsView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'logs',
            nav: 'logs',
          },
        },
        {
          path: '/usage-logs/task',
          name: 'logs-tasks',
          component: () => import('@/views/console/TaskLogsView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'logs',
            nav: 'logs',
          },
        },
        {
          path: '/usage-logs/operations',
          name: 'logs-operations',
          component: () => import('@/views/console/OperationLogsView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            requiresAdmin: true,
            feature: 'logs',
            nav: 'logs',
          },
        },
        {
          path: '/channels',
          name: 'channels',
          component: () => import('@/views/console/ChannelsView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'admin',
            ...getConsoleRouteAccessMeta('channels'),
          },
        },
        {
          path: '/ticket-management/:id?',
          name: 'ticket-management',
          component: () => import('@/views/console/AdminTicketsView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'admin',
            ...getConsoleRouteAccessMeta('ticket-management'),
          },
        },
        {
          path: '/users',
          name: 'users',
          component: () => import('@/views/console/UsersView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'admin',
            ...getConsoleRouteAccessMeta('users'),
          },
        },
        {
          path: '/redemption-codes',
          name: 'redemption',
          component: () => import('@/views/console/RedemptionView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'admin',
            ...getConsoleRouteAccessMeta('redemption'),
          },
        },
        {
          path: '/plan-management',
          name: 'plan-management',
          component: () => import('@/views/console/PlanManagementView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            protected: true,
            feature: 'subscription_balance',
            ...getConsoleRouteAccessMeta('plan-management'),
          },
        },
        {
          path: '/orders',
          name: 'orders',
          component: () => import('@/views/console/OrdersView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'orders',
            ...getConsoleRouteAccessMeta('orders'),
          },
        },
        {
          path: '/tickets',
          name: 'tickets',
          component: () => import('@/views/console/TicketsView.vue'),
          meta: {
            noPageScroll: true,
            feature: 'tickets',
          },
        },
        {
          path: '/tickets/:id',
          name: 'ticket-detail',
          component: () => import('@/views/console/TicketDetailView.vue'),
          meta: { nav: 'tickets', feature: 'tickets' },
        },
        {
          path: '/wallet',
          name: 'wallet',
          component: () => import('@/views/console/WalletView.vue'),
          meta: { feature: 'wallet' },
        },
        {
          path: '/subscription',
          name: 'subscription',
          component: () => import('@/views/console/SubscriptionView.vue'),
          meta: { protected: true, feature: 'subscription_balance' },
        },
        {
          path: '/invite',
          name: 'invite',
          component: () => import('@/views/console/InviteView.vue'),
          meta: { feature: 'invites' },
        },
        {
          path: '/invoice',
          name: 'invoice',
          component: () => import('@/views/console/InvoiceView.vue'),
          meta: { protected: true, feature: 'invoices' },
        },
        {
          path: '/settings',
          name: 'settings',
          component: () => import('@/views/console/AccountSettingsView.vue'),
          meta: { feature: 'profile' },
        },
        {
          path: '/system-settings',
          name: 'system-settings',
          redirect: {
            name: 'system-settings-site',
            params: { section: 'system-info' },
          },
          component: () => import('@/views/console/SystemSettingsView.vue'),
          meta: { wide: true, feature: 'admin', requiresRoot: true },
          children: [
            {
              path: 'site/:section?',
              name: 'system-settings-site',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'site' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
            {
              path: 'auth/:section?',
              name: 'system-settings-auth',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'auth' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
            {
              path: 'billing/:section?',
              name: 'system-settings-billing',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'billing' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
            {
              path: 'models/:section?',
              name: 'system-settings-models',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'models' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
            {
              path: 'security/:section?',
              name: 'system-settings-security',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'security' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
            {
              path: 'content/:section?',
              name: 'system-settings-content',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'content' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
            {
              path: 'operations/:section?',
              name: 'system-settings-operations',
              component: () =>
                import('@/components/console/systemSettings/SystemSettingsDomainView.vue'),
              props: { domain: 'operations' },
              meta: { wide: true, feature: 'admin', requiresRoot: true },
            },
          ],
        },
        {
          path: '/profile',
          name: 'profile',
          component: () => import('@/views/console/AccountCenterView.vue'),
          meta: { feature: 'profile' },
        },
        {
          path: '/farm',
          name: 'farm',
          component: () => import('@/views/console/FarmView.vue'),
          meta: {
            topNav: 'activities',
            protected: true,
            feature: 'farm',
          },
        },
        {
          path: '/bigame',
          name: 'bigame',
          component: () => import('@/views/console/BigameView.vue'),
          meta: {
            topNav: 'activities',
            protected: true,
            feature: 'bigame',
          },
        },
      ],
    },
    {
      path: '/lab',
      component: () => import('@/components/layout/LabLayout.vue'),
      meta: {
        requiresAuth: true,
        topNav: 'alchemy',
        protected: true,
        feature: 'lab',
        messageDomain: 'lab',
      },
      children: [
        { path: '', redirect: { name: 'lab-chat' } },
        {
          path: 'chat',
          name: 'lab-chat',
          component: () => import('@/views/lab/ChatView.vue'),
        },
        {
          path: 'chat/:id',
          name: 'lab-chat-session',
          component: () => import('@/views/lab/ChatView.vue'),
          meta: { nav: 'lab-chat' },
        },
        {
          path: 'studio',
          name: 'lab-studio',
          component: () => import('@/views/lab/StudioView.vue'),
        },
        {
          path: 'assets',
          name: 'lab-assets',
          component: () => import('@/views/lab/AssetsView.vue'),
        },
        {
          path: 'notes',
          name: 'lab-notes',
          component: () => import('@/views/lab/NotesView.vue'),
        },
        {
          path: 'plugins',
          name: 'lab-plugins',
          component: () => import('@/views/lab/PluginsView.vue'),
        },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { public: true },
    },
  ],
  scrollBehavior() {
    return { top: 0 }
  },
})

router.beforeEach(async (to) => {
  const query = new URL(to.fullPath, window.location.origin).searchParams
  const canonical = canonicalLegacyPath(to.path, query)
  if (canonical !== to.path)
    return { path: canonical, query: to.query, hash: to.hash }
  if (to.meta.setupError) {
    await loadMessageDomain('setup')
    return true
  }

  if (to.meta.setupRoute) await loadMessageDomain('setup')
  const setup = useSetupStore()
  try {
    const setupStatus = await setup.load()
    if (!setupStatus.status && !to.meta.setupRoute) {
      return { name: 'setup' }
    }
    if (setupStatus.status && to.meta.setupRoute) {
      return { name: 'home' }
    }
  } catch {
    return {
      name: 'setup-error',
      query: { redirect: to.fullPath },
    }
  }

  if (to.meta.messageDomain === 'lab') {
    await Promise.all([loadMessageDomain('console'), loadMessageDomain('lab')])
  } else if (to.meta.messageDomain) {
    await loadMessageDomain(to.meta.messageDomain)
  }

  if (to.meta.feature || to.name === 'sign-up') {
    const app = useAppStore()
    await app.initialize()
    const featureUnavailable =
      to.meta.feature &&
      (!app.statusReachable || !app.isFeatureEnabled(to.meta.feature))
    if (featureUnavailable) {
      return to.name === 'dashboard' ? { name: 'home' } : CONSOLE_ENTRY
    }
    if (to.name === 'sign-up' && app.statusReachable && !app.registerEnabled) {
      return { name: 'sign-in' }
    }
  }

  if (!to.meta.requiresAuth && !to.meta.guestOnly) return true

  const auth = useAuthStore()
  if (!auth.checked) await auth.fetchSelf()

  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    return {
      name: 'sign-in',
      query: {
        redirect: sanitizeRedirect(to.fullPath) ?? '/dashboard',
      },
    }
  }
  if (to.meta.guestOnly && auth.isAuthenticated) return CONSOLE_ENTRY
  if (to.meta.requiresRoot && !auth.isRoot) return CONSOLE_ENTRY
  if (to.meta.requiresAdmin && !auth.isAdmin) return CONSOLE_ENTRY
  if (
    to.meta.requiresPermission &&
    !auth.hasPermission(
      to.meta.requiresPermission.resource,
      to.meta.requiresPermission.action
    )
  ) {
    return CONSOLE_ENTRY
  }
  return true
})

router.onError((error) => {
  navigationError.value = true
  navigationPending.value = false
  console.error('[router] Navigation failed', error)
  const chunkFailed =
    /Failed to fetch dynamically imported module|Importing a module script failed/i.test(
      error.message
    )
  try {
    if (!chunkFailed || window.sessionStorage.getItem(CHUNK_RELOAD_KEY) === '1')
      return
    window.sessionStorage.setItem(CHUNK_RELOAD_KEY, '1')
    window.location.reload()
  } catch {
    // Storage may be unavailable; the visible error state still allows retry.
  }
})

router.afterEach((_to, _from, failure) => {
  navigationPending.value = false
  if (failure) return
  navigationError.value = false
  try {
    window.sessionStorage.removeItem(CHUNK_RELOAD_KEY)
  } catch {
    // Storage is optional for navigation.
  }
})

export default router
