import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

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

vi.mock('@/api/public', () => ({ publicApi }))
vi.mock('@/api/setup', () => ({ setupApi }))

import { ApiError } from '@/api/types'
import router from '@/router'
import { sanitizeSetupRedirect, sanitizeRedirect } from '@/router'
import { navigationError } from '@/router/navigationState'
import { useAuthStore } from '@/stores/auth'
import { useSetupStore } from '@/stores/setup'

beforeEach(async () => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  setupApi.status.mockResolvedValue({
    status: true,
    root_init: true,
    database_type: 'postgres',
  })
  publicApi.notice.mockResolvedValue('')
  publicApi.pricing.mockResolvedValue([])
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
    available_quota: '100',
    reserved_quota: '0',
    quota_version: 0,
    used_quota: '0',
    request_count: 0,
    created_at: 1_700_000_000,
  })
  auth.checked = true

  await router.push('/')
})

describe('capability route guard', () => {
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

  // docs/authentication.md: a temporary refresh failure must leave the
  // session unchecked so a later navigation retries it.
  it.each(['/sign-in', '/pricing'])(
    'keeps a rate-limited session check on %s retryable',
    async (path) => {
      vi.spyOn(console, 'error').mockImplementation(() => undefined)
      const auth = useAuthStore()
      auth.checked = false
      vi.spyOn(auth, 'fetchSelf').mockRejectedValue(
        new ApiError('Too many requests', { status: 429 })
      )

      await router.push(path).catch(() => undefined)

      expect(router.currentRoute.value.name).toBe('home')
      expect(navigationError.value).toBe('rate-limited')
      expect(auth.checked).toBe(false)
    }
  )

  it('fails closed for protected routes when status is unreachable', async () => {
    publicApi.status.mockRejectedValue(new Error('status unavailable'))

    await router.push('/console/market')

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
      available_quota: '100',
      reserved_quota: '0',
      quota_version: 0,
      used_quota: '0',
      request_count: 0,
      created_at: 1_700_000_000,
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

    for (const path of [
      '/console/market',
      '/console/invoice',
      '/console/keys/11/routing',
      '/console/farm',
      '/console/bigame',
    ]) {
      await router.push(path)
      expect(router.currentRoute.value.name, path).toBe('dashboard')
    }
  })

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
      '/ops',
      '/system-info',
    ]) {
      await router.push(path)
      expect(router.currentRoute.value.name, path).toBe('dashboard')
    }
  })

  it('opens the ops monitor for administrators only', async () => {
    await router.push('/ops')
    expect(router.currentRoute.value.name).toBe('dashboard')
    useAuthStore().user!.role = 10
    await router.push('/ops')
    expect(router.currentRoute.value.name).toBe('ops-monitor')
    expect(router.currentRoute.value.meta.topNav).toBe('ops')
  }, 15000)

  it('sends retired system instance bookmarks to the ops monitor', async () => {
    useAuthStore().user!.role = 10
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
