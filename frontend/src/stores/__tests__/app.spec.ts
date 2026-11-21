import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const publicApi = vi.hoisted(() => ({
  status: vi.fn(),
  notice: vi.fn(),
  pricingCount: vi.fn(),
  uptime: vi.fn(),
}))

vi.mock('@/api/public', () => ({ publicApi }))

import { useAuthStore } from '@/stores/auth'
import type { PublicStatus } from '@/api/public'
import { useAppStore } from '@/stores/app'

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().checked = true
  vi.clearAllMocks()
  publicApi.status.mockResolvedValue({ system_name: 'Ren2Hub' })
  publicApi.notice.mockResolvedValue('Notice')
  publicApi.pricingCount.mockResolvedValue(1)
  publicApi.uptime.mockResolvedValue([
    { monitors: [{ uptime: 0.999, status: 1 }] },
  ])
})

// The store listens on document; dispose it so later tests do not see it.
afterEach(() => useAppStore().$dispose())

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

  it('keeps the first status version as the page version and flags a newer server', async () => {
    const store = useAppStore()
    publicApi.status.mockResolvedValue({ version: 'v1.0.0' })
    await store.initialize()
    await store.refreshStatus()
    expect(store.serverVersionChanged).toBe(false)

    publicApi.status.mockResolvedValue({ version: 'v1.0.1' })
    await store.refreshStatus()
    expect(store.serverVersionChanged).toBe(true)
    expect(store.versionLabel).toBe('v1.0.1')
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

  it('reports the status as known while the home summaries are still loading', async () => {
    publicApi.notice.mockReturnValue(new Promise(() => {}))
    const store = useAppStore()

    await store.whenStatusKnown()

    expect(store.statusReachable).toBe(true)
    expect(store.phase).toBe('loading')
    expect(publicApi.status).toHaveBeenCalledOnce()
  })

  it('reports an unreachable status as known and retries on the next call', async () => {
    publicApi.status.mockRejectedValueOnce(new Error('offline'))
    const store = useAppStore()

    await store.whenStatusKnown()
    expect(store.statusReachable).toBe(false)
    expect(store.phase).toBe('error')

    await store.whenStatusKnown()
    expect(store.statusReachable).toBe(true)
    expect(publicApi.status).toHaveBeenCalledTimes(2)
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
    expect(publicApi.pricingCount).not.toHaveBeenCalled()
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
    expect(publicApi.pricingCount).toHaveBeenCalledTimes(1)
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
    await vi.waitFor(() =>
      expect(publicApi.pricingCount).toHaveBeenCalledTimes(1)
    )
  })

  it('exposes loading, disabled, protected, failed and zero counts distinctly', async () => {
    const store = useAppStore()
    useAuthStore().checked = false
    await store.initialize()
    expect(store.modelCountState).toBe('loading')
    expect(publicApi.pricingCount).not.toHaveBeenCalled()
    store.status.HeaderNavModules = '{"pricing":false}'
    expect(store.modelCountState).toBe('notPublic')
    store.status.HeaderNavModules = '{"pricing":{"requireAuth":true}}'
    useAuthStore().checked = true
    expect(store.modelCountState).toBe('signIn')
    publicApi.pricingCount.mockRejectedValueOnce(new Error('offline'))
    store.status.HeaderNavModules = ''
    await vi.waitFor(() => expect(store.modelCountState).toBe('error'))
    store.statusReachable = false
    publicApi.pricingCount.mockResolvedValueOnce(0)
    await store.refreshStatus()
    await vi.waitFor(() => expect(store.modelCountState).toBe('ready'))
    expect(store.modelCountLabel).toBe('0')
  })

  it('reports a genuinely empty catalog as a measured zero count', async () => {
    publicApi.pricingCount.mockResolvedValueOnce(0)
    const store = useAppStore()

    await store.initialize()

    expect(store.modelCountState).toBe('ready')
    expect(store.modelCountLabel).toBe('0')
    expect(store.phase).toBe('ready')
  })

  it('keeps a failed status unavailable after session discovery completes', async () => {
    const auth = useAuthStore()
    auth.checked = false
    publicApi.status.mockRejectedValueOnce(new Error('status offline'))
    const store = useAppStore()

    await store.initialize()
    expect(store.phase).toBe('error')
    expect(store.modelCountState).toBe('error')
    const failureLabel = store.modelCountLabel

    auth.checked = true
    await Promise.resolve()

    expect(store.modelCountState).toBe('error')
    expect(store.modelCountLabel).toBe(failureLabel)
    expect(publicApi.pricingCount).not.toHaveBeenCalled()
  })

  it('discards protected counts when permission changes before a response arrives', async () => {
    const store = useAppStore()
    await store.initialize()
    let resolve!: (value: number) => void
    publicApi.pricingCount.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    store.status.HeaderNavModules = '{"pricing":true}'
    // The access rule did not change, so authenticate to start a different audience request.
    useAuthStore().persist({ id: 3, role: 1 } as never)
    const signal = publicApi.pricingCount.mock.calls.at(-1)![0] as AbortSignal
    store.status.HeaderNavModules = '{"pricing":false}'
    expect(signal.aborted).toBe(true)
    resolve(2)
    await vi.waitFor(() => expect(store.modelCountState).toBe('notPublic'))
    await Promise.resolve()
    expect(store.modelCountState).toBe('notPublic')
  })

  it('applies the docs module permission and safe URL policy to the shared entry', () => {
    const store = useAppStore()
    store.status = {
      docs_link: '/docs',
      HeaderNavModules: '{"docs":{"requireAuth":true}}',
    }
    expect(store.docsNavigation.disabledReason).toBe('nav.loginRequired')
    useAuthStore().persist({ id: 1, role: 1 } as never)
    expect(store.docsNavigation.href).toBe(
      new URL('/docs', location.origin).href
    )
    store.status.docs_link = 'javascript:alert(1)'
    expect(store.docsNavigation.disabledReason).toBe('nav.invalidLink')
    store.status.docs_link = ''
    expect(store.docsNavigation.disabledReason).toBe('nav.notConfigured')
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

describe('status revalidation when the page becomes visible', () => {
  const FIVE_MINUTES = 5 * 60 * 1000
  let visibility: DocumentVisibilityState = 'visible'

  function show(state: DocumentVisibilityState) {
    visibility = state
    document.dispatchEvent(new Event('visibilitychange'))
  }

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-11-21T00:00:00Z'))
    visibility = 'visible'
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => visibility,
    })
  })
  afterEach(() => vi.useRealTimers())

  async function openedAt(version: string) {
    const store = useAppStore()
    publicApi.status.mockResolvedValue({ version })
    await store.refreshStatus()
    publicApi.status.mockClear()
    return store
  }

  it('re-reads a stale status when the page becomes visible and flags a new version', async () => {
    const store = await openedAt('v1')
    publicApi.status.mockResolvedValue({ version: 'v2' })
    vi.setSystemTime(Date.now() + FIVE_MINUTES + 1)
    show('visible')
    await vi.waitFor(() => expect(store.serverVersionChanged).toBe(true))
    expect(publicApi.status).toHaveBeenCalledOnce()
  })

  it('does not re-read within five minutes of the last read', async () => {
    await openedAt('v1')
    vi.setSystemTime(Date.now() + FIVE_MINUTES - 1)
    show('visible')
    await Promise.resolve()
    expect(publicApi.status).not.toHaveBeenCalled()
  })

  it('does not re-read when the page becomes hidden', async () => {
    await openedAt('v1')
    vi.setSystemTime(Date.now() + FIVE_MINUTES + 1)
    show('hidden')
    await Promise.resolve()
    expect(publicApi.status).not.toHaveBeenCalled()
  })

  it('sends one request at a time and retries on the next visibility after a failure', async () => {
    const store = await openedAt('v1')
    let fail!: (error: Error) => void
    publicApi.status.mockReturnValueOnce(
      new Promise((_resolve, reject) => {
        fail = reject
      })
    )
    vi.setSystemTime(Date.now() + FIVE_MINUTES + 1)
    show('visible')
    show('visible')
    expect(publicApi.status).toHaveBeenCalledOnce()
    fail(new Error('offline'))
    await vi.waitFor(() => expect(store.statusReachable).toBe(true))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(store.serverVersionChanged).toBe(false)

    publicApi.status.mockResolvedValue({ version: 'v2' })
    show('visible')
    await vi.waitFor(() => expect(store.serverVersionChanged).toBe(true))
    expect(publicApi.status).toHaveBeenCalledTimes(2)
  })

  it('stops listening once the store is disposed', async () => {
    const store = await openedAt('v1')
    store.$dispose()
    vi.setSystemTime(Date.now() + FIVE_MINUTES + 1)
    show('visible')
    await Promise.resolve()
    expect(publicApi.status).not.toHaveBeenCalled()
  })
})
