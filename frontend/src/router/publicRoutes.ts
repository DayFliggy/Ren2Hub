import type {
  RouteLocationNormalized,
  RouteLocationRaw,
  RouteRecordRaw,
} from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

export async function guardPublicModule(
  to: RouteLocationNormalized
): Promise<true | RouteLocationRaw> {
  const app = useAppStore()
  try {
    await app.refreshStatus()
  } catch {
    return { name: 'status-503' }
  }
  const access = app.moduleAccess(String(to.meta.publicModule))
  if (!access.enabled) return { name: 'status-404' }
  // A failed session check (for example a rate-limited refresh) propagates to
  // the router's navigation error state like every other guarded route, so the
  // page stays retryable instead of being reported as an unavailable service.
  const auth = useAuthStore()
  if (!auth.checked) await auth.fetchSelf()
  if (access.requireAuth && !auth.isAuthenticated)
    return { name: 'sign-in', query: { redirect: to.fullPath } }
  return true
}

export async function guardLegalDocument(
  to: RouteLocationNormalized
): Promise<true | RouteLocationRaw> {
  const app = useAppStore()
  try {
    await app.refreshStatus()
  } catch {
    return { name: 'status-503' }
  }
  const enabled =
    to.name === 'privacy-policy'
      ? app.privacyPolicyEnabled
      : app.userAgreementEnabled
  return enabled ? true : { name: 'status-404' }
}

export const publicRoutes: RouteRecordRaw[] = [
  ...(['about', 'privacy-policy', 'user-agreement'] as const).map((kind) => ({
    path: `/${kind}`,
    name: kind,
    component: () => import('@/views/public/PublicDocumentView.vue'),
    props: { kind },
    meta: {
      public: true,
      ...(kind === 'about' ? { publicModule: 'about' } : {}),
    },
    beforeEnter: kind === 'about' ? guardPublicModule : guardLegalDocument,
  })),
  {
    path: '/pricing',
    name: 'pricing',
    component: () => import('@/views/public/PricingView.vue'),
    meta: { public: true, publicModule: 'pricing' },
    beforeEnter: guardPublicModule,
  },
  {
    path: '/pricing/:modelId',
    name: 'pricing-model',
    component: () => import('@/views/public/PricingView.vue'),
    meta: { public: true, publicModule: 'pricing' },
    beforeEnter: guardPublicModule,
  },
  {
    path: '/rankings',
    name: 'rankings',
    component: () => import('@/views/public/RankingsView.vue'),
    meta: { public: true, publicModule: 'rankings' },
    beforeEnter: guardPublicModule,
  },
  ...([401, 403, 404, 500, 503] as const).map((status) => ({
    path: `/${status}`,
    name: `status-${status}`,
    component: () => import('@/views/public/StatusErrorView.vue'),
    props: { status },
    meta: { public: true },
  })),
]
