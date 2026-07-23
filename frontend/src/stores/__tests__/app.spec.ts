import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const publicApi = vi.hoisted(() => ({
  status: vi.fn(),
  notice: vi.fn(),
  pricing: vi.fn(),
  uptime: vi.fn(),
}))

vi.mock('@/api/public', () => ({ publicApi }))

import { parseHeaderModules, useAppStore } from '@/stores/app'

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
  it('normalizes HeaderNavModules objects and JSON strings', () => {
    expect(parseHeaderModules('{"pricing":{"enabled":false}}')).toEqual({
      pricing: { enabled: false },
    })
    expect(parseHeaderModules('{bad json')).toEqual({})
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
})
