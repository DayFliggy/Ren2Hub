import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'

vi.mock('@/api/console', () => ({ api: { get: vi.fn(), put: vi.fn() } }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn() }),
}))

beforeEach(() => {
  vi.resetModules()
  vi.resetAllMocks()
})

describe('system settings load result', () => {
  it('uses authoritative structured defaults before an option is stored', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        {
          key: 'payment_setting.amount_options',
          value_type: 'json',
          default_value: [10, 20, 50, 100, 200, 500],
        },
        {
          key: 'TopupGroupRatio',
          value_type: 'json',
          default_value: { default: 1, vip: 1, svip: 1 },
        },
      ])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(true)
    expect(settings.rawValue('payment_setting.amount_options', '[]')).toBe(
      '[10,20,50,100,200,500]'
    )
    expect(settings.rawValue('TopupGroupRatio', '{}')).toBe(
      '{"default":1,"vip":1,"svip":1}'
    )
  })
  it('keeps descriptor-backed integration options in the shared settings snapshot', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'GmpayId', value: 'merchant-1' }])
      .mockResolvedValueOnce([
        {
          key: 'GmpayId',
          value_type: 'string',
          default_value: '',
          editor: 'text',
          sensitive: false,
          editable: true,
        },
      ])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(true)
    expect((settings.settings.value as Record<string, unknown>).GmpayId).toBe(
      'merchant-1'
    )
  })
  it('rejects malformed or unavailable configuration metadata without enabling edits', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([])
      .mockRejectedValueOnce(new Error('catalog unavailable'))
    expect(await settings.load()).toBe(false)
    expect(await settings.saveOptions({ Price: 9 })).toBe(false)
    expect(api.put).not.toHaveBeenCalled()
    vi.mocked(api.get)
      .mockResolvedValueOnce({})
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(false)
  })

  it('preserves numeric-looking identifiers and exact quota strings despite inferred numeric descriptors', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([
        { key: 'EpayId', value: '0009007199254740993' },
        { key: 'QuotaForNewUser', value: '9007199254740993' },
      ])
      .mockResolvedValueOnce([
        {
          key: 'EpayId',
          value_type: 'number',
          default_value: 9007199254740992,
        },
        {
          key: 'QuotaForNewUser',
          value_type: 'number',
          default_value: 9007199254740992,
        },
      ])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(true)
    expect(settings.rawValue('EpayId', '')).toBe('0009007199254740993')
    expect(settings.settings.value.QuotaForNewUser).toBe('9007199254740993')
  })

  it('keeps successful secret writes out of shared caches and retains blank credentials', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(true)
    vi.mocked(api.put).mockResolvedValue(null)
    expect(
      await settings.saveOptions({ EpayKey: 'replacement', Price: 8 })
    ).toBe(true)
    expect(settings.rawOptions.value).toEqual({ Price: '8' })
    expect(settings.rawValue('EpayKey')).toBe('')
    expect(settings.isSecretConfigured('EpayKey')).toBe(true)
    expect(await settings.updateOption('EpayKey', ' ')).toBe(true)
    expect(api.put).toHaveBeenCalledTimes(1)
  })

  it('prevents overlapping saves and a failed write preserves the prior snapshot', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'Price', value: '7.3' }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(true)
    let reject!: (error: Error) => void
    vi.mocked(api.put).mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail
        })
    )
    const first = settings.saveOptions({ EpayKey: 'failed-secret', Price: 8 })
    expect(settings.saving.value).toBe(true)
    expect(await settings.saveOptions({ Price: 9 })).toBe(false)
    reject(new Error('storage failed'))
    expect(await first).toBe(false)
    expect(settings.rawValue('Price', 7.3)).toBe(7.3)
    expect(settings.isSecretConfigured('EpayKey')).toBe(false)
    expect(settings.saving.value).toBe(false)
  })

  it('waits for a pending write before reloading its authoritative values', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: [] })
    expect(await settings.load()).toBe(true)
    let finish!: (value: null) => void
    vi.mocked(api.put).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const saving = settings.saveOptions({ Price: 8 })
    const refresh = settings.load(true)
    expect(api.get).toHaveBeenCalledTimes(3)
    vi.mocked(api.get)
      .mockResolvedValueOnce([{ key: 'Price', value: '8' }])
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce({ configured: [] })
    finish(null)
    expect(await saving).toBe(true)
    expect(await refresh).toBe(true)
    expect(settings.rawValue('Price', 7.3)).toBe(8)
  })
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
