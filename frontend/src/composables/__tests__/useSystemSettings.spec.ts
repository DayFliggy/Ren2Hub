import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'

vi.mock('@/api/console', () => ({ api: { get: vi.fn() } }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn() }),
}))

beforeEach(() => {
  vi.resetModules()
  vi.resetAllMocks()
})

describe('system settings load result', () => {
  it('waits for an in-flight read and fetches again when a write requires fresh settings', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    let finish!: (value: unknown) => void
    vi.mocked(api.get)
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve
          })
      )
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: [] })
      .mockResolvedValueOnce([{ key: 'GmpayId', value: 'new-merchant' }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: ['GmpayKey'] })
    const initial = settings.load()
    const shared = settings.load()
    const afterSave = settings.load(true)
    expect(api.get).toHaveBeenCalledOnce()
    finish([{ key: 'GmpayId', value: 'old-merchant' }])
    expect(await initial).toBe(true)
    expect(await shared).toBe(true)
    expect(await afterSave).toBe(true)
    expect(settings.rawValue('GmpayId')).toBe('new-merchant')
    expect(settings.isSecretConfigured('GmpayKey')).toBe(true)
    expect(
      vi.mocked(api.get).mock.calls.filter(([path]) => path === '/api/option/')
    ).toHaveLength(2)
  })

  it('reports initial failure and retries without marking defaults as loaded', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get).mockRejectedValueOnce(new Error('unavailable'))
    expect(await settings.load()).toBe(false)
    expect(settings.loaded.value).toBe(false)
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'GmpayId', value: 'merchant' }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: ['GmpayKey'] })
    expect(await settings.load()).toBe(true)
    expect(settings.rawValue('GmpayId')).toBe('merchant')
    expect(settings.isSecretConfigured('GmpayKey')).toBe(true)
    const requests = vi.mocked(api.get).mock.calls.length
    expect(await settings.load()).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(requests)
  })

  it('preserves the complete last snapshot if secret-status refresh fails', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'GmpayId', value: 'old-merchant' }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: ['GmpayKey'] })
    expect(await settings.load()).toBe(true)
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'GmpayId', value: 'new-merchant' }])
      .mockResolvedValueOnce([])
      .mockRejectedValueOnce(new Error('secret status unavailable'))
    expect(await settings.load(true)).toBe(false)
    expect(settings.rawValue('GmpayId')).toBe('old-merchant')
    expect(settings.isSecretConfigured('GmpayKey')).toBe(true)
    expect(settings.loading.value).toBe(false)
    expect(settings.loaded.value).toBe(false)
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'GmpayId', value: 'new-merchant' }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: ['GmpayKey'] })
    expect(await settings.load()).toBe(true)
    expect(settings.rawValue('GmpayId')).toBe('new-merchant')
  })
})
