import {
  createRouter,
  createWebHistory,
  parseQuery,
  stringifyQuery,
  type RouteLocationRaw,
} from 'vue-router'

import HomeView from '@/views/HomeView.vue'
import {
  getConsoleRouteAccessMeta,
  tokenRoutingAccess,
} from '@/constants/navigation/consoleNav'
import { getTopNavRouteMeta } from '@/constants/navigation/topNav'
import { loadMessageDomain } from '@/i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useSetupStore } from '@/stores/setup'
import { publicRoutes } from '@/router/publicRoutes'
import { adminRoutes } from '@/router/adminRoutes'
import { canonicalLegacyLocation } from '@/router/legacyRoutes'
import { navigationError } from '@/router/navigationState'
import { ApiError } from '@/api/types'

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
    const target = canonicalLegacyLocation(url.pathname, parseQuery(url.search))
    if (target.path === '/setup/error') return null
    return router.resolve({ ...target, hash: url.hash }).fullPath
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
    const target = router.resolve({
      ...canonicalLegacyLocation(url.pathname, parseQuery(url.search)),
      hash: url.hash,
    })
    if (
      !target.matched.length ||
      target.name === 'not-found' ||
      target.meta.guestOnly ||
      target.meta.setupRoute ||
      target.meta.setupError
    )
      return null
    return target.fullPath
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
          path: '/ops',
          name: 'ops-monitor',
          component: () => import('@/views/admin/OpsMonitorView.vue'),
          meta: {
            wide: true,
            requiresRoot: true,
            ...getTopNavRouteMeta('ops'),
          },
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
          meta: { feature: 'user_models', wide: true },
        },

        {
          path: '/keys',
          name: 'keys',
          component: () => import('@/views/console/KeysView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'api_tokens',
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
            ...tokenRoutingAccess,
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
          // Members read their own events here; reading other accounts needs
          // the audit permission and is enforced by the server.
          path: '/usage-logs/audit',
          name: 'logs-audit',
          component: () => import('@/views/console/AuditLogsView.vue'),
          meta: {
            wide: true,
            noPageScroll: true,
            feature: 'audit',
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
          path: '/invite',
          name: 'invite',
          component: () => import('@/views/console/InviteView.vue'),
          meta: { feature: 'invites' },
        },

        {
          path: '/settings',
          name: 'settings',
          component: () => import('@/views/console/AccountSettingsView.vue'),
          meta: { feature: 'profile' },
        },
        {
          path: '/security',
          name: 'security',
          component: () => import('@/views/console/SecurityAccessView.vue'),
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
          // Every settings domain highlights the one sidebar entry.
          meta: {
            wide: true,
            feature: 'admin',
            requiresRoot: true,
            nav: 'system-settings-site',
          },
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
  const canonical = canonicalLegacyLocation(to.path, to.query)
  if (
    canonical.path !== to.path ||
    stringifyQuery(canonical.query) !== stringifyQuery(to.query)
  )
    return {
      ...canonical,
      hash: to.hash,
      replace: true,
    }
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

  if (to.meta.messageDomain) await loadMessageDomain(to.meta.messageDomain)
  // SecurityAccessView uses the auth domain for password fields. The route is
  // reachable directly, so its lazy auth messages must be present before the
  // first render rather than relying on a previous sign-in visit.
  if (to.name === 'security') await loadMessageDomain('auth')

  // Home metrics may require a session; restore it before deciding which
  // transport and visibility rules apply, including after a full refresh.
  if (!to.meta.requiresAuth && !to.meta.guestOnly && to.name !== 'home')
    return true

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

// Chromium, Firefox and Safari script failures, and Vite's CSS preload failure.
const CHUNK_LOAD_ERROR =
  /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Unable to preload CSS/i

router.onError((error, to) => {
  navigationError.value = {
    kind:
      error instanceof ApiError && error.status === 429
        ? 'rate-limited'
        : 'failed',
    href: router.resolve(to.fullPath).href,
  }
  console.error('[router] Navigation failed', error)
  if (!(error instanceof Error) || !CHUNK_LOAD_ERROR.test(error.message)) return
  // Load the page that failed once from the server; if it fails again, the
  // marker is still set and the error state stays.
  try {
    if (window.sessionStorage.getItem(CHUNK_RELOAD_KEY) === '1') return
    window.sessionStorage.setItem(CHUNK_RELOAD_KEY, '1')
  } catch {
    // Without a persistent marker, reloading could loop indefinitely.
    return
  }
  window.location.assign(navigationError.value.href)
})

router.afterEach((_to, _from, failure) => {
  if (failure) return
  navigationError.value = null
  try {
    window.sessionStorage.removeItem(CHUNK_RELOAD_KEY)
  } catch {
    // Storage is optional for navigation.
  }
})

export default router
