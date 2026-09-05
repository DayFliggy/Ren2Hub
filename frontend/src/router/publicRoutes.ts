import type {
  RouteLocationNormalized,
  RouteLocationRaw,
  RouteRecordRaw,
} from 'vue-router'
import { publicApi } from '@/api/public'
import { isRecord } from '@/api/contracts'
import { useAuthStore } from '@/stores/auth'
import { parseHeaderModules } from '@/stores/app'

export function publicModuleAccess(value: unknown): {
  enabled: boolean
  requireAuth: boolean
} {
  const bool = (item: unknown, fallback: boolean) => {
    if (typeof item === 'string') item = item.trim().toLowerCase()
    if (item === true || item === 1 || item === '1' || item === 'true')
      return true
    if (item === false || item === 0 || item === '0' || item === 'false')
      return false
    return fallback
  }
  return isRecord(value)
    ? {
        enabled: bool(value.enabled, true),
        requireAuth: bool(value.requireAuth, false),
      }
    : { enabled: bool(value, true), requireAuth: false }
}

export async function guardPublicModule(
  to: RouteLocationNormalized
): Promise<true | RouteLocationRaw> {
  try {
    const status = await publicApi.status()
    const access = publicModuleAccess(
      parseHeaderModules(status.HeaderNavModules)[String(to.meta.publicModule)]
    )
    if (!access.enabled) return { name: 'status-404' }
    const auth = useAuthStore()
    if (!auth.checked) await auth.fetchSelf()
    if (access.requireAuth && !auth.isAuthenticated)
      return { name: 'sign-in', query: { redirect: to.fullPath } }
    return true
  } catch {
    return { name: 'status-503' }
  }
}

export async function guardLegalDocument(
  to: RouteLocationNormalized
): Promise<true | RouteLocationRaw> {
  try {
    const status = await publicApi.status()
    const enabled =
      to.name === 'privacy-policy'
        ? status.privacy_policy_enabled
        : status.user_agreement_enabled
    return enabled ? true : { name: 'status-404' }
  } catch {
    return { name: 'status-503' }
  }
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
