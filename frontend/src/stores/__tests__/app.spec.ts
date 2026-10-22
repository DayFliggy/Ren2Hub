import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const publicApi = vi.hoisted(() => ({
  status: vi.fn(),
  notice: vi.fn(),
  pricing: vi.fn(),
  uptime: vi.fn(),
}))

vi.mock('@/api/public', () => ({ publicApi }))

import { useAuthStore } from '@/stores/auth'
import type { PublicStatus } from '@/api/public'
import { useAppStore } from '@/stores/app'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
  publicApi.status.mockResolvedValue({ system_name: 'Ren2Hub' })
  publicApi.notice.mockResolvedValue('Notice')
  publicApi.pricing.mockResolvedValue([{ id: 1 }])
  publicApi.uptime.mockResolvedValue([
    { monitors: [{ uptime: 0.999, status: 1 }] },
  ])
})

describe('public application state', () => {
  it('uses the landing reference brand before status is available', () => {
    expect(useAppStore().systemName).toBe('Ren2Hub')
  })

  const open = { enabled: true, requireAuth: false }
  const closed = { enabled: false, requireAuth: false }

  // Expected values follow middleware/header_nav.go, which the backend uses
  // to enforce the same option.
  it.each([
    ['{"pricing":" FALSE "}', 'pricing', closed],
    ['{"pricing":" TRUE "}', 'pricing', open],
    ['{"pricing":{"enabled":"0"}}', 'pricing', closed],
    [
      '{"pricing":{"enabled":true,"requireAuth":1}}',
      'pricing',
      { enabled: true, requireAuth: true },
    ],
    [
      '{"pricing":{"requireAuth":" True "}}',
      'pricing',
      { enabled: true, requireAuth: true },
    ],
    ['{"pricing":2}', 'pricing', open],
    ['{"docs":{"enabled":false}}', 'docs', closed],
    ['{"about":{"enabled":" false "}}', 'about', closed],
    ['[]', 'pricing', open],
    ['null', 'pricing', open],
    ['not-json', 'pricing', open],
    ['', 'pricing', open],
  ])('reads HeaderNavModules %s for %s', (raw, module, access) => {
    const store = useAppStore()
    store.status = { HeaderNavModules: raw }

    expect(store.moduleAccess(module)).toEqual(access)
  })

  it('hides docs and about entries the option disables', () => {
    const store = useAppStore()
    store.status = {
      HeaderNavModules: '{"docs":{"enabled":false},"about":{"enabled":"0"}}',
      docs_link: '/docs',
    }
    expect(store.showDocs).toBe(false)
    expect(store.showAbout).toBe(false)

    store.status = {
      HeaderNavModules: '[]',
      docs_link: '/docs',
    }
    expect(store.showDocs).toBe(true)
    expect(store.showAbout).toBe(true)
  })

  it('keeps the newest status when an earlier refresh resolves last', async () => {
    const pending: Array<(value: PublicStatus) => void> = []
    publicApi.status.mockImplementation(
      () => new Promise((resolve) => pending.push(resolve))
    )
    const store = useAppStore()
    const older = store.refreshStatus()
    const newer = store.refreshStatus()

    pending[1]!({ HeaderNavModules: '{"pricing":false}' })
    await newer
    pending[0]!({ HeaderNavModules: '' })
    await older

    expect(store.moduleAccess('pricing')).toEqual(closed)
    expect(store.statusReachable).toBe(true)
  })

  it('distinguishes reachable status from degraded summaries', async () => {
    publicApi.notice.mockRejectedValueOnce(new Error('notice unavailable'))
    const store = useAppStore()

    await store.initialize()

    expect(store.statusReachable).toBe(true)
    expect(store.phase).toBe('degraded')
    expect(store.online).toBe(false)
  })

  it('allows a failed first initialization to retry', async () => {
    publicApi.status.mockRejectedValueOnce(new Error('offline'))
    const store = useAppStore()

    await store.initialize()
    expect(store.phase).toBe('error')

    await store.retry()
    expect(store.phase).toBe('ready')
    expect(store.online).toBe(true)
  })

  it('shares one in-flight initialization across callers', async () => {
    let resolveStatus!: (value: { system_name: string }) => void
    publicApi.status.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveStatus = resolve
        })
    )
    const store = useAppStore()

    const first = store.initialize()
    const second = store.initialize()
    expect(publicApi.status).toHaveBeenCalledTimes(1)

    resolveStatus({ system_name: 'Ren2Hub' })
    await Promise.all([first, second])
    expect(store.phase).toBe('ready')
  })

  it('enforces registration and authenticated-pricing flags', async () => {
    publicApi.status.mockResolvedValueOnce({
      register_enabled: false,
      HeaderNavModules: '{"pricing":{"enabled":true,"requireAuth":true}}',
    })
    const store = useAppStore()

    await store.initialize()

    expect(store.registerEnabled).toBe(false)
    expect(store.showPricing).toBe(false)
    expect(publicApi.pricing).not.toHaveBeenCalled()
  })

  it('loads protected pricing for an authenticated demo session', async () => {
    useAuthStore().persist({
      id: 1,
      username: 'demo',
      display_name: 'Demo',
      email: 'demo@example.com',
      role: 1,
      status: 1,
      quota: 100,
      used_quota: 0,
      request_count: 0,
    })
    publicApi.status.mockResolvedValueOnce({
      HeaderNavModules: '{"pricing":{"enabled":true,"requireAuth":true}}',
    })
    const store = useAppStore()

    await store.initialize()

    expect(store.showPricing).toBe(true)
    expect(publicApi.pricing).toHaveBeenCalledTimes(1)
  })

  it('loads protected pricing when authentication changes during initialization', async () => {
    let resolveNotice!: (value: string) => void
    publicApi.notice.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveNotice = resolve
        })
    )
    publicApi.status.mockResolvedValueOnce({
      HeaderNavModules: '{"pricing":{"enabled":true,"requireAuth":true}}',
    })
    const store = useAppStore()
    const initialization = store.initialize()
    await vi.waitFor(() => expect(publicApi.notice).toHaveBeenCalledTimes(1))

    useAuthStore().persist({
      id: 1,
      username: 'demo',
      display_name: 'Demo',
      email: 'demo@example.com',
      role: 1,
      status: 1,
      quota: 100,
      used_quota: 0,
      request_count: 0,
    })
    resolveNotice('Notice')
    await initialization
    await vi.waitFor(() => expect(publicApi.pricing).toHaveBeenCalledTimes(1))
  })

  it('skips the uptime request when the administrator disabled the panel', async () => {
    publicApi.status.mockResolvedValueOnce({ uptime_kuma_enabled: false })
    const store = useAppStore()

    await store.initialize()

    expect(publicApi.uptime).not.toHaveBeenCalled()
    expect(store.uptimePercent).toBeNull()
    expect(store.phase).toBe('ready')
  })

  it('clamps malformed uptime ratios to the documented range', async () => {
    publicApi.uptime.mockResolvedValueOnce([
      { monitors: [{ uptime: 4, status: 1 }] },
    ])
    const store = useAppStore()

    await store.initialize()

    expect(store.uptimeLabel).toBe('100.00%')
  })

  it('resolves live, disabled, and missing capabilities', async () => {
    publicApi.status.mockResolvedValueOnce({
      frontend_capabilities: {
        wallet: 'live',
        marketplace: 'disabled',
        passkey: 'disabled',
      },
    })
    const store = useAppStore()

    await store.initialize()

    expect(store.featureStatus('wallet', 'disabled')).toBe('live')
    expect(store.featureStatus('marketplace', 'live')).toBe('disabled')
    expect(store.featureStatus('passkey', 'live')).toBe('disabled')
    expect(store.featureStatus('unknown', 'disabled')).toBe('disabled')
    expect(store.isFeatureEnabled('passkey')).toBe(false)
    expect(store.isFeatureEnabled('unknown')).toBe(false)
  })
})
