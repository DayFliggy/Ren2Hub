import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const setupApi = vi.hoisted(() => ({
  status: vi.fn(),
  submit: vi.fn(),
}))
vi.mock('@/api/setup', () => ({ setupApi }))

import router from '@/router'
import { navigationError } from '@/router/navigationState'

const nativeLocation = window.location
const assign = vi.fn()

function brokenRoute(error: unknown) {
  router.addRoute({
    path: '/broken-chunk',
    name: 'broken-chunk',
    component: () => Promise.reject(error),
    meta: { public: true },
  })
}

async function openBrokenRoute() {
  await router.push('/broken-chunk').catch(() => undefined)
}

beforeEach(async () => {
  setActivePinia(createPinia())
  setupApi.status.mockResolvedValue({
    status: true,
    root_init: true,
    database_type: 'postgres',
  })
  vi.spyOn(console, 'error').mockImplementation(() => undefined)
  sessionStorage.clear()
  await router.push('/')
  navigationError.value = null
  // Only the reload target matters; history navigation stays in memory.
  Object.defineProperty(window, 'location', {
    value: { ...nativeLocation, origin: nativeLocation.origin, assign },
    configurable: true,
    writable: true,
  })
})

afterEach(() => {
  Object.defineProperty(window, 'location', {
    value: nativeLocation,
    configurable: true,
    writable: true,
  })
  if (router.hasRoute('broken-chunk')) router.removeRoute('broken-chunk')
  sessionStorage.clear()
  assign.mockReset()
})

describe('route chunk load failures', () => {
  it.each([
    'Failed to fetch dynamically imported module: /assets/WalletView-old.js',
    'error loading dynamically imported module: /assets/WalletView-old.js',
    'Importing a module script failed.',
    'Unable to preload CSS for /assets/WalletView-old.css',
  ])(
    'loads the failed page once, then keeps the error state: %s',
    async (message) => {
      brokenRoute(new Error(message))

      await openBrokenRoute()
      expect(assign).toHaveBeenCalledTimes(1)
      expect(assign).toHaveBeenCalledWith('/broken-chunk')

      // The reloaded page fails the same way: no second reload.
      await openBrokenRoute()
      expect(assign).toHaveBeenCalledTimes(1)
      expect(navigationError.value).toBe('failed')
    }
  )

  it('shows the error state without reloading for other failures', async () => {
    brokenRoute(new Error('Render failed'))

    await openBrokenRoute()

    expect(assign).not.toHaveBeenCalled()
    expect(navigationError.value).toBe('failed')
  })

  it('does not reload when session storage cannot hold the marker', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('Storage blocked')
    })
    brokenRoute(new Error('Unable to preload CSS for /assets/x.css'))

    await openBrokenRoute()

    expect(assign).not.toHaveBeenCalled()
    expect(navigationError.value).toBe('failed')
  })
})
