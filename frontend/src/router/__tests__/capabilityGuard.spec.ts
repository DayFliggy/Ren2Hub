import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import { START_LOCATION } from 'vue-router'

const publicApi = vi.hoisted(() => ({
  status: vi.fn(),
  notice: vi.fn(),
  pricing: vi.fn(),
  uptime: vi.fn(),
}))

const setupApi = vi.hoisted(() => ({
  status: vi.fn(),
  submit: vi.fn(),
}))

const authApi = vi.hoisted(() => ({
  refreshSession: vi.fn(),
  self: vi.fn(),
}))

vi.mock('@/api/auth', () => ({ authApi }))
vi.mock('@/api/public', () => ({ publicApi }))
vi.mock('@/api/publicCatalog', () => ({
  publicCatalogApi: { pricing: publicApi.pricing },
}))
vi.mock('@/api/setup', () => ({ setupApi }))

import { clearAuthBundle } from '@/api/authSession'
import { ApiError } from '@/api/types'
import router from '@/router'
import { sanitizeSetupRedirect, sanitizeRedirect } from '@/router'
import { navigationError } from '@/router/navigationState'
import { guardPublicModule } from '@/router/publicRoutes'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useSetupStore } from '@/stores/setup'

function setSessionHint(present: boolean) {
  document.cookie = present
    ? 'new_api_has_session=1; path=/'
    : 'new_api_has_session=; path=/; max-age=0'
}

function sessionBundle(
  user: NonNullable<ReturnType<typeof useAuthStore>['user']>
) {
  return {
    access_token: 'access-token',
    token_type: 'Bearer',
    access_expires_at: 2_000_000_000,
    session: {
      sid: 'session-1',
      current: true,
      login_method: 'password',
      ip: '127.0.0.1',
      user_agent: 'vitest',
      created_at: 1,
      last_active_at: 2,
      expires_at: 2_000_000_000,
    },
    user,
  }
}

beforeEach(async () => {
  setSessionHint(false)
  clearAuthBundle()
  setActivePinia(createPinia())
  vi.clearAllMocks()
  setupApi.status.mockResolvedValue({
    status: true,
    root_init: true,
    database_type: 'postgres',
  })
  publicApi.notice.mockResolvedValue('')
  publicApi.pricing.mockResolvedValue({ models: [], issues: [] })
  publicApi.uptime.mockResolvedValue([])
  publicApi.status.mockResolvedValue({
    frontend_capabilities: {
      dashboard_basic: 'live',
      user_models: 'live',
      logs: 'live',
      admin: 'live',
      wallet: 'live',
      api_tokens: 'live',
      profile: 'live',
      registration: 'live',
    },
  })

  const auth = useAuthStore()
  auth.persist({
    id: 1,
    username: 'user',
    display_name: 'User',
    email: 'user@example.com',
    role: 1,
    status: 1,
    quota: 100,
    used_quota: 0,
    request_count: 0,
  })
  auth.checked = true

  await router.push('/')
})

describe('capability route guard', () => {
  it('blocks the sign-up route when global registration is disabled', async () => {
    const auth = useAuthStore()
    auth.persist(null)
    auth.checked = true
    publicApi.status.mockResolvedValue({
      register_enabled: false,
      frontend_capabilities: { registration: 'live' },
    })
    await router.push('/sign-up')
    expect(router.currentRoute.value.name).toBe('sign-in')
  })
  it.each([1, 10])(
    'keeps GMPay merchant configuration inaccessible to role %s',
    async (role) => {
      useAuthStore().user!.role = role
      await router.push('/system-settings/billing/gmpay')
      expect(router.currentRoute.value.name).toBe('dashboard')
    },
    15000
  )

  it.each([
    '/subscription',
    '/plan-management',
    '/plans',
    '/console/subscription',
    '/console/plan-management',
    '/console/plans',
  ])('resolves retired plan page %s to the standard 404', async (path) => {
    const auth = useAuthStore()
    auth.user!.role = 100

    await router.push(path)

    expect(router.currentRoute.value.name).toBe('not-found')
    expect(sanitizeRedirect(path)).toBeNull()
    expect(publicApi.status).not.toHaveBeenCalled()
  })

  it('redirects uninitialized routes to setup before auth checks', async () => {
    const setup = useSetupStore()
    setup.phase = 'idle'
    setup.status = null
    setupApi.status.mockResolvedValue({
      status: false,
      root_init: false,
      database_type: 'sqlite',
    })

    await router.push('/console/models')

    expect(router.currentRoute.value.name).toBe('setup')
  })

  it('redirects setup status failures to the global setup error page', async () => {
    const setup = useSetupStore()
    setup.phase = 'idle'
    setup.status = null
    setupApi.status.mockRejectedValue(new Error('setup unavailable'))

    await router.push('/console/models')

    expect(router.currentRoute.value.name).toBe('setup-error')
    expect(router.currentRoute.value.query.redirect).toBe('/models')
  })

  it('redirects initialized setup visits to the Vue home page', async () => {
    await router.push('/setup')

    expect(router.currentRoute.value.name).toBe('home')
  })

  it('rejects unsafe setup redirect targets', () => {
    expect(sanitizeSetupRedirect('https://evil.example/')).toBeNull()
    expect(sanitizeSetupRedirect('//evil.example/')).toBeNull()
    expect(sanitizeSetupRedirect('/setup/error')).toBeNull()
    expect(sanitizeSetupRedirect('/auth/sign-in')).toBe('/sign-in')
    expect(sanitizeSetupRedirect('/next/console/dashboard?tab=1')).toBe(
      '/404?tab=1'
    )
  })

  it('waits for the cookie session check before entering home', async () => {
    setSessionHint(true)
    await router.push('/404')
    const auth = useAuthStore()
    const user = auth.user!
    auth.persist(null)
    auth.checked = false
    let finish!: () => void
    authApi.refreshSession.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = () => resolve(sessionBundle(user))
        })
    )
    const entering = router.push('/')
    await flushPromises()
    expect(authApi.refreshSession).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.name).toBe('status-404')
    finish()
    await entering
    expect(router.currentRoute.value.name).toBe('home')
    expect(auth.isAuthenticated).toBe(true)
  })

  it.each(['/', '/sign-in', '/pricing'])(
    'renders %s as a guest without a refresh when the session hint is absent',
    async (path) => {
      await router.push('/404')
      const auth = useAuthStore()
      auth.persist(null)
      auth.checked = false

      await router.push(path)

      expect(authApi.refreshSession).not.toHaveBeenCalled()
      expect(router.currentRoute.value.path).toBe(path)
      expect(auth.checked).toBe(true)
      expect(auth.isAuthenticated).toBe(false)
    }
  )

  // A missing hint is not a server verdict: a protected page still asks.
  it('still refreshes for a protected page when the session hint is absent', async () => {
    await router.push('/404')
    const auth = useAuthStore()
    const user = auth.user!
    auth.persist(null)
    auth.checked = false
    await router.push('/pricing')
    authApi.refreshSession.mockResolvedValueOnce(sessionBundle(user))

    await router.push('/keys')

    expect(authApi.refreshSession).toHaveBeenCalledOnce()
    expect(router.currentRoute.value.path).toBe('/keys')
  })

  // A public page renders either way, so a rate-limited refresh shows the
  // guest view; the first protected page asks the server again.
  it.each([
    '/sign-in?redirect=%2Fkeys%3Fpage%3D2#auth',
    '/pricing?model=gpt%2F4&unit=K#prices',
    '/?session-check=1#home',
  ])(
    'renders %s as a guest when the session check is rate-limited',
    async (path) => {
      vi.spyOn(console, 'warn').mockImplementation(() => undefined)
      setSessionHint(true)
      await router.push('/404')
      const auth = useAuthStore()
      const user = auth.user!
      auth.persist(null)
      auth.checked = false
      authApi.refreshSession.mockRejectedValueOnce(
        new ApiError('Too many requests', { status: 429 })
      )

      await router.push(path)

      expect(router.currentRoute.value.fullPath).toBe(path)
      expect(navigationError.value).toBeNull()
      expect(auth.isAuthenticated).toBe(false)

      authApi.refreshSession.mockResolvedValueOnce(sessionBundle(user))
      await router.push('/keys')
      expect(authApi.refreshSession).toHaveBeenCalledTimes(2)
      expect(router.currentRoute.value.path).toBe('/keys')
    }
  )

  it('keeps a rate-limited session check on a protected page retryable', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    await router.push('/404')
    const auth = useAuthStore()
    auth.persist(null)
    auth.checked = false
    authApi.refreshSession.mockRejectedValue(
      new ApiError('Too many requests', { status: 429 })
    )

    await router.push('/keys?page=2#list').catch(() => undefined)

    expect(router.currentRoute.value.name).toBe('status-404')
    expect(navigationError.value).toEqual({
      kind: 'rate-limited',
      href: '/keys?page=2#list',
    })
    expect(auth.checked).toBe(false)
  })

  it('fails closed for protected routes when status is unreachable', async () => {
    publicApi.status.mockRejectedValue(new Error('status unavailable'))

    await router.push('/console/models')

    expect(router.currentRoute.value.name).toBe('home')
  }, 15000)

  it('fails closed for every module when status is unreachable', async () => {
    publicApi.status.mockRejectedValue(new Error('status unavailable'))

    await router.push('/console/models')

    expect(router.currentRoute.value.name).toBe('home')
  })

  it('keeps the audit log page behind the audit capability', async () => {
    await router.push('/usage-logs/audit')

    expect(router.currentRoute.value.name).toBe('dashboard')
  }, 15000)

  it('sends retired audit and operation-log bookmarks to the audit logs page', async () => {
    publicApi.status.mockResolvedValue({
      frontend_capabilities: { dashboard_basic: 'live', audit: 'live' },
    })
    for (const [source, target] of [
      ['/audit', '/usage-logs/audit'],
      ['/console/audit', '/usage-logs/audit'],
      // The old operation-log pages listed every account's events.
      ['/console/logs/operations', '/usage-logs/audit?scope=all'],
      ['/usage-logs/operations/', '/usage-logs/audit?scope=all'],
      ['/usage-logs/operations?scope=self', '/usage-logs/audit?scope=self'],
    ]) {
      await router.push(source!)
      expect(router.currentRoute.value.fullPath, source).toBe(target)
      expect(router.currentRoute.value.name, source).toBe('logs-audit')
    }
  }, 15000)

  it('keeps system settings root-only even for ordinary administrators', async () => {
    const auth = useAuthStore()
    auth.persist({
      id: 2,
      username: 'admin',
      display_name: 'Admin',
      email: 'admin@example.com',
      role: 10,
      status: 1,
      quota: 100,
      used_quota: 0,
      request_count: 0,
    })
    auth.checked = true

    await router.push('/console/system-settings/site')

    expect(router.currentRoute.value.name).toBe('dashboard')
  })

  it('redirects every deferred module when its capability is disabled', async () => {
    publicApi.status.mockResolvedValue({
      frontend_capabilities: {
        dashboard_basic: 'live',
        marketplace: 'disabled',
        invoices: 'disabled',
        farm: 'disabled',
        bigame: 'disabled',
      },
    })

    for (const path of ['/console/keys/11/routing']) {
      await router.push(path)
      expect(router.currentRoute.value.name, path).toBe('dashboard')
    }
  })

  // Only root bypasses permissions, so a user and an admin share one matrix.
  it.each([
    [1, false, 'dashboard'],
    [1, true, 'token-routing'],
    [10, false, 'dashboard'],
    [10, true, 'token-routing'],
  ])(
    'opens token routing for role %i only with routing:read %s',
    async (role, read, routeName) => {
      publicApi.status.mockResolvedValue({
        frontend_capabilities: {
          dashboard_basic: 'live',
          token_private_routing: 'live',
        },
      })
      useAuthStore().user!.role = role
      // authz.Capabilities lists every registered resource for the user.
      useAuthStore().user!.permissions = {
        admin_permissions: { routing: { read, write: read } },
      }

      await router.push('/keys/11/routing')

      expect(router.currentRoute.value.name).toBe(routeName)
    },
    15000
  )

  it('keeps canonical routes at the root and preserves legacy queries and anchors', async () => {
    for (const [source, target] of [
      ['/console/token', '/keys'],
      ['/console/wallet', '/wallet'],
      ['/console/logs/drawing', '/usage-logs/drawing'],
      ['/usage-logs?tab=tasks', '/usage-logs/task?tab=tasks'],
      ['/console/personal', '/profile'],
    ]) {
      await router.push(
        `${source}${source!.includes('?') ? '&' : '?'}p=2#details`
      )
      expect(router.currentRoute.value.fullPath).toBe(
        `${target}${target!.includes('?') ? '&' : '?'}p=2#details`
      )
    }
    expect(sanitizeRedirect('/wallet?topup=success#records')).toBe(
      '/wallet?topup=success#records'
    )
    expect(sanitizeRedirect('/next/console/token')).toBe('/404')
    expect(sanitizeRedirect('/next//evil.example')).toBe('/404')
    expect(sanitizeRedirect('//evil.example')).toBeNull()
  })

  it('denies all migrated management pages when admin capability is disabled', async () => {
    const auth = useAuthStore()
    auth.user!.role = 100
    publicApi.status.mockResolvedValue({
      frontend_capabilities: { admin: 'disabled', dashboard_basic: 'live' },
    })
    for (const path of [
      '/models/metadata',
      '/models/vendors',
      '/models/pricing',
      '/models/prefill-groups',
      '/system-settings/billing/topup-discount',
      '/ops',
      '/system-info',
    ]) {
      await router.push(path)
      expect(router.currentRoute.value.name, path).toBe('dashboard')
    }
  })

  it('opens the native performance monitor for root only', async () => {
    await router.push('/ops')
    expect(router.currentRoute.value.name).toBe('dashboard')
    useAuthStore().user!.role = 100
    await router.push('/ops')
    expect(router.currentRoute.value.name).toBe('ops-monitor')
    expect(router.currentRoute.value.meta.topNav).toBe('ops')
  }, 15000)

  it('sends retired system instance bookmarks to the ops monitor', async () => {
    useAuthStore().user!.role = 100
    for (const path of [
      '/system-info',
      '/console/system-info',
      '/admin/system-info',
      '/console/admin/system-info',
    ]) {
      await router.push('/dashboard')
      await router.push(path)
      expect(router.currentRoute.value.path, path).toBe('/ops')
    }
    expect(sanitizeRedirect('/admin/system-info')).toBe('/ops')
  }, 15000)

  it('never revives retired pages through old prefixes or settings sections', async () => {
    for (const path of [
      '/next',
      '/next/',
      '/next/keys',
      '/next/console/admin/vendors',
      '/next/assets/app.js',
      '/console/next/keys',
      '/chat',
      '/next/console/chat/1',
      '/console/playground',
      '/system-settings/content/chat-presets',
      '/console/setting?tab=chats',
      '/models/deployments',
      '/deployment',
      '/admin/deployments',
      '/console/models/deployments',
      '/next/console/admin/deployments/1',
      '/next/deployment',
    ]) {
      await router.push(path)
      expect(router.currentRoute.value.name, path).toBe('status-404')
    }
  })

  it('does not register retired marketplace, invoice, farm, game, or gateway pages', async () => {
    for (const path of [
      '/market',
      '/invoice',
      '/farm',
      '/bigame',
      '/gateway',
      '/request-diagnostics',
    ]) {
      await router.push(path)
      expect(router.currentRoute.value.name, path).toBe('not-found')
    }
  })

  it('opens model management sections from bookmarks without losing query or hash', async () => {
    useAuthStore().user!.role = 10
    // Vendors is its own section, as in NewAPI; prefill groups open as a dialog.
    for (const [source, path, manage] of [
      ['/models/vendors', '/models/vendors', 'other'],
      ['/console/admin/vendors', '/models/vendors', 'other'],
      ['/models/prefill-groups', '/models/metadata', 'prefill-groups'],
      ['/console/admin/prefill-groups', '/models/metadata', 'prefill-groups'],
    ]) {
      await router.push(`${source}?q=alpha&p=2&manage=other#details`)
      expect(router.currentRoute.value.path).toBe(path)
      expect(router.currentRoute.value.query).toEqual({
        q: 'alpha',
        ...(path === '/models/metadata' ? { page: '2' } : { p: '2' }),
        manage,
      })
      expect(router.currentRoute.value.hash).toBe('#details')
    }
    expect(router.currentRoute.value.meta.nav).toBeUndefined()
    await router.push('/models/vendors')
    // The vendors section highlights the one "Models" sidebar entry.
    expect(router.currentRoute.value.meta.nav).toBe('models-admin')
  })

  it('normalizes old model filters during navigation on the same route', async () => {
    useAuthStore().user!.role = 10
    await router.push('/models/metadata?filter=initial')
    await router.push(
      '/models/metadata?keyword=gpt&sync_official=0&p=2&page_size=50&vendor=3&tag=a&tag=b#models'
    )
    expect(router.currentRoute.value.path).toBe('/models/metadata')
    expect(router.currentRoute.value.query).toEqual({
      filter: 'gpt',
      sync: '0',
      page: '2',
      pageSize: '50',
      vendor: '3',
      tag: ['a', 'b'],
    })
    expect(router.currentRoute.value.hash).toBe('#models')
  })

  it('prefers canonical model filter keys and removes their old aliases', async () => {
    useAuthStore().user!.role = 10
    await router.push(
      '/console/admin/models?keyword=old&filter=new&sync_official=0&sync=1&p=2&page=3&page_size=50&pageSize=20&extra=yes#models'
    )
    expect(router.currentRoute.value.path).toBe('/models/metadata')
    expect(router.currentRoute.value.query).toEqual({
      filter: 'new',
      sync: '1',
      page: '3',
      pageSize: '20',
      extra: 'yes',
    })
    expect(router.currentRoute.value.hash).toBe('#models')
  })

  it('opens former vendor management links through the same redirect contract', async () => {
    useAuthStore().user!.role = 10
    const source =
      '/console/admin/models?manage=vendors&keyword=gpt&vFilter=open&extra=yes#vendors'
    await router.push(source)
    expect(router.currentRoute.value.path).toBe('/models/vendors')
    expect(router.currentRoute.value.query).toEqual({
      filter: 'gpt',
      vFilter: 'open',
      extra: 'yes',
    })
    expect(router.currentRoute.value.hash).toBe('#vendors')
    expect(sanitizeRedirect(source)).toBe(router.currentRoute.value.fullPath)
    expect(sanitizeSetupRedirect(source)).toBe(
      router.currentRoute.value.fullPath
    )
  })

  it('redirects former automatic pricing settings to the model pricing page', async () => {
    useAuthStore().user!.role = 100
    for (const path of [
      '/system-settings/models/auto-pricing',
      '/system-settings/billing/pricing',
      '/system-settings/billing/model-pricing',
      '/console/setting?tab=ratio',
      '/system-settings/models/deployment',
      '/console/system-settings/models/model-deployment',
      '/console/setting?tab=model-deployment',
    ]) {
      await router.push(
        `${path}${path.includes('?') ? '&' : '?'}audit=1#pricing`
      )
      expect(router.currentRoute.value.path).toBe('/models/pricing')
      expect(router.currentRoute.value.name).toBe('models-pricing')
      expect(router.currentRoute.value.meta.nav).toBe('models-admin')
      expect(router.currentRoute.value.query.audit).toBe('1')
      expect(router.currentRoute.value.hash).toBe('#pricing')
    }
  })

  it('opens top-up discounts for root only', async () => {
    for (const role of [1, 10]) {
      useAuthStore().user!.role = role
      await router.push('/system-settings/billing/topup-discount')
      expect(router.currentRoute.value.name, `${role}`).toBe('dashboard')
    }
    useAuthStore().user!.role = 100
    await router.push('/system-settings/billing/topup-discount?audit=1#rules')
    expect(router.currentRoute.value.name).toBe('system-settings-billing')
    expect(router.currentRoute.value.path).toBe(
      '/system-settings/billing/topup-discount'
    )
    expect(router.currentRoute.value.meta.nav).toBe('system-settings-site')
    expect(router.currentRoute.value.query.audit).toBe('1')
    expect(router.currentRoute.value.hash).toBe('#rules')
  })

  it('keeps model pricing on the model management workspace for root only', async () => {
    useAuthStore().user!.role = 10
    await router.push('/models/pricing')
    expect(router.currentRoute.value.name).toBe('dashboard')
    useAuthStore().user!.role = 100
    await router.push('/models/pricing')
    expect(router.currentRoute.value.path).toBe('/models/pricing')
    expect(router.currentRoute.value.meta.nav).toBe('models-admin')
  })
})

describe('public module guard', () => {
  it('sends a module the option disables to 404 like the navigation', async () => {
    publicApi.status.mockResolvedValue({
      HeaderNavModules: '{"rankings":{"enabled":" false "}}',
    })

    await router.push('/rankings')

    expect(router.currentRoute.value.name).toBe('status-404')
    expect(useAppStore().moduleAccess('rankings').enabled).toBe(false)
  })

  it('asks a visitor to sign in for a module that requires it', async () => {
    useAuthStore().persist(null)
    publicApi.status.mockResolvedValue({
      HeaderNavModules: '{"rankings":{"requireAuth":1}}',
    })

    await router.push('/rankings')

    expect(router.currentRoute.value.name).toBe('sign-in')
    expect(router.currentRoute.value.query.redirect).toBe('/rankings')
  })

  it('reuses the startup status read for the first navigation only', async () => {
    publicApi.status.mockClear()
    const app = useAppStore()
    // main.ts starts initialization alongside the first navigation.
    const startup = app.initialize()
    // Guards receive normalized locations, whose name is never null.
    const pricing = { ...router.resolve('/pricing'), name: 'pricing' }

    expect(await guardPublicModule(pricing, START_LOCATION)).toBe(true)
    await startup
    expect(publicApi.status).toHaveBeenCalledOnce()

    expect(await guardPublicModule(pricing, router.currentRoute.value)).toBe(
      true
    )
    expect(publicApi.status).toHaveBeenCalledTimes(2)
  })

  it('updates the navigation when the guard reads a changed option', async () => {
    const app = useAppStore()
    await app.initialize()
    expect(app.showPricing).toBe(true)
    publicApi.status.mockResolvedValue({
      HeaderNavModules: '{"pricing":false}',
    })

    await router.push('/pricing')

    expect(router.currentRoute.value.name).toBe('status-404')
    expect(app.showPricing).toBe(false)
  })
})
