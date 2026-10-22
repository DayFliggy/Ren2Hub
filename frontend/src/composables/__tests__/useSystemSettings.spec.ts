import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
const toast = vi.hoisted(() => ({ error: vi.fn() }))
const app = vi.hoisted(() => ({ refreshStatus: vi.fn() }))
vi.mock('@/api/console', () => ({ api: { get: vi.fn(), put: vi.fn() } }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
beforeEach(() => {
  vi.resetModules()
  vi.resetAllMocks()
})
async function setup(options: Array<{ key: string; value: string }> = []) {
  const { useSystemSettings } = await import('../useSystemSettings')
  const settings = useSystemSettings()
  vi.mocked(api.get).mockResolvedValue(options)
  expect(await settings.load()).toBe(true)
  return settings
}
describe('native per-key settings', () => {
  it('reads the native list and preserves identifiers and large quota strings', async () => {
    const settings = await setup([
      { key: 'EpayId', value: '0009007199254740993' },
      { key: 'QuotaForNewUser', value: '9007199254740993' },
    ])
    expect(api.get).toHaveBeenCalledExactlyOnceWith('/api/option/')
    expect(settings.rawValue('EpayId')).toBe('0009007199254740993')
    expect(settings.rawValue('QuotaForNewUser')).toBe('9007199254740993')
    expect(settings.isSecretConfigured('EpayKey')).toBe(false)
  })
  it('loads invalid stored option values raw instead of replacing them with defaults', async () => {
    const settings = await setup([
      { key: 'Price', value: 'bad' },
      { key: 'CheckSensitiveEnabled', value: 'maybe' },
      { key: 'RetryTimes', value: '' },
    ])
    expect(settings.loaded.value).toBe(true)
    expect(settings.rawValue('Price', 7.3)).toBe('bad')
    expect(settings.rawValue('CheckSensitiveEnabled', false)).toBe('maybe')
    expect(settings.rawValue('RetryTimes', 0)).toBe('')
    expect(settings.rawValue('QuotaPerUnit', 500000)).toBe(500000)
    expect(toast.error).not.toHaveBeenCalled()
  })
  it('parses valid stored scalars against the field default type', async () => {
    const settings = await setup([
      { key: 'Price', value: '8' },
      { key: 'CheckSensitiveEnabled', value: 'true' },
      { key: 'DrawingEnabled', value: '0' },
    ])
    expect(settings.rawValue('Price', 7.3)).toBe(8)
    expect(settings.rawValue('CheckSensitiveEnabled', false)).toBe(true)
    expect(settings.rawValue('DrawingEnabled', true)).toBe(false)
  })
  it('does not replace the last valid snapshot with a malformed response or mark it loaded', async () => {
    const settings = await setup([{ key: 'Price', value: '7.3' }])
    for (const data of [
      [{ key: 'ResendKeyConfigured', value: 'maybe' }],
      { data: [] },
      [{ key: 'Price', value: 7 }],
    ]) {
      vi.mocked(api.get).mockResolvedValueOnce(data)
      expect(await settings.load(true)).toBe(false)
      expect(settings.loaded.value).toBe(false)
      expect(settings.rawValue('Price')).toBe('7.3')
    }
    expect(toast.error).toHaveBeenCalledTimes(3)
  })
  it('writes parameters before enabling a dependent provider and keeps secrets out of caches', async () => {
    const settings = await setup()
    vi.mocked(api.put).mockResolvedValue(null)
    expect(
      await settings.saveOptions({
        GitHubOAuthEnabled: true,
        GitHubClientId: 'client',
        GitHubClientSecret: 'secret',
      })
    ).toBe(true)
    expect(vi.mocked(api.put).mock.calls.map(([, body]) => body)).toEqual([
      { key: 'GitHubClientId', value: 'client' },
      { key: 'GitHubClientSecret', value: 'secret' },
      { key: 'GitHubOAuthEnabled', value: 'true' },
    ])
    expect(settings.rawValue('GitHubClientSecret')).toBe('')
    expect(settings.rawOptions.value).not.toHaveProperty('GitHubClientSecret')
    expect(settings.isSecretConfigured('GitHubClientSecret')).toBe(true)
  })
  it('retains successful writes and stops before enabling when a parameter fails', async () => {
    const settings = await setup()
    const onApplied = vi.fn()
    vi.mocked(api.put)
      .mockResolvedValueOnce(null)
      .mockRejectedValueOnce(new Error('rejected'))
    expect(
      await settings.saveOptions(
        {
          GitHubOAuthEnabled: true,
          GitHubClientId: 'client',
          GitHubClientSecret: 'secret',
        },
        { onApplied }
      )
    ).toBe(false)
    expect(api.put).toHaveBeenCalledTimes(2)
    expect(onApplied).toHaveBeenCalledExactlyOnceWith(
      'GitHubClientId',
      'client',
      false
    )
    expect(settings.rawValue('GitHubClientId')).toBe('client')
    expect(settings.rawValue('GitHubOAuthEnabled')).toBe('')
    expect(toast.error).toHaveBeenCalledOnce()
    expect(settings.saving.value).toBe(false)
  })
  it('marks a saved Resend key from its read-only status without resubmitting the status', async () => {
    const settings = await setup([
      { key: 'ResendFrom', value: 'noreply@example.com' },
      { key: 'ResendKeyConfigured', value: 'true' },
    ])
    expect(settings.isSecretConfigured('ResendApiKey')).toBe(true)
    expect(settings.rawValue('ResendApiKey')).toBe('')
    expect(settings.rawOptions.value).not.toHaveProperty('ResendKeyConfigured')
    vi.mocked(api.put).mockResolvedValue(null)
    expect(await settings.saveOptions({ ResendFrom: 'mail@example.com' })).toBe(
      true
    )
    expect(vi.mocked(api.put).mock.calls.map(([, body]) => body)).toEqual([
      { key: 'ResendFrom', value: 'mail@example.com' },
    ])
    vi.mocked(api.get).mockResolvedValueOnce([
      { key: 'ResendKeyConfigured', value: 'false' },
    ])
    expect(await settings.load(true)).toBe(true)
    expect(settings.isSecretConfigured('ResendApiKey')).toBe(false)
  })
  it('does not overwrite saved credentials with a blank input', async () => {
    const settings = await setup()
    expect(await settings.saveOptions({ EpayKey: ' ' })).toBe(true)
    expect(api.put).not.toHaveBeenCalled()
  })
  it.each(['HeaderNavModules', 'SidebarModulesAdmin'])(
    'refreshes the shared navigation after saving %s',
    async (key) => {
      const settings = await setup()
      vi.mocked(api.put).mockResolvedValue(null)
      expect(await settings.saveOptions({ [key]: '{}' })).toBe(true)
      expect(app.refreshStatus).toHaveBeenCalledOnce()
      expect(settings.rawValue(key)).toBe('{}')
    }
  )
  it('refreshes navigation once after both options commit', async () => {
    const settings = await setup()
    vi.mocked(api.put).mockResolvedValue(null)
    app.refreshStatus.mockImplementationOnce(async () => {
      expect(api.put).toHaveBeenCalledTimes(2)
      expect(settings.saving.value).toBe(true)
    })
    expect(
      await settings.saveOptions({
        HeaderNavModules: '{"pricing":false}',
        SidebarModulesAdmin: '{"admin":{"channel":false}}',
      })
    ).toBe(true)
    expect(app.refreshStatus).toHaveBeenCalledOnce()
    expect(settings.saving.value).toBe(false)
  })
  it('refreshes committed navigation even when a later option fails', async () => {
    const settings = await setup()
    vi.mocked(api.put)
      .mockResolvedValueOnce(null)
      .mockRejectedValueOnce(new Error('rejected'))
    expect(
      await settings.saveOptions({ HeaderNavModules: '{}', RetryTimes: 3 })
    ).toBe(false)
    expect(settings.rawValue('HeaderNavModules')).toBe('{}')
    expect(settings.rawValue('RetryTimes')).toBe('')
    expect(app.refreshStatus).toHaveBeenCalledOnce()
  })
  it.each([
    { HeaderNavModules: '{}' },
    { RetryTimes: 3, HeaderNavModules: '{}' },
  ])('does not refresh uncommitted navigation options', async (patch) => {
    const settings = await setup()
    vi.mocked(api.put).mockRejectedValueOnce(new Error('rejected'))
    expect(await settings.saveOptions(patch)).toBe(false)
    expect(app.refreshStatus).not.toHaveBeenCalled()
  })
  it('does not refresh navigation for unrelated saved options', async () => {
    const settings = await setup()
    vi.mocked(api.put).mockResolvedValue(null)
    expect(await settings.saveOptions({ RetryTimes: 3 })).toBe(true)
    expect(app.refreshStatus).not.toHaveBeenCalled()
  })
  it.each([
    ['en', 'Navigation settings were saved', 'Reload the page'],
    ['zh-CN', '导航设置已保存', '请重新加载页面'],
  ] as const)(
    'reports a failed navigation refresh in %s without rolling back the saved option',
    async (locale, savedMessage, retryMessage) => {
      const settings = await setup()
      const { loadMessageDomain, setLocale } = await import('@/i18n')
      await loadMessageDomain('console')
      await setLocale(locale)
      const onApplied = vi.fn()
      vi.mocked(api.put).mockResolvedValue(null)
      app.refreshStatus.mockRejectedValueOnce(new Error('offline'))
      expect(
        await settings.saveOptions({ HeaderNavModules: '{}' }, { onApplied })
      ).toBe(false)
      expect(settings.rawValue('HeaderNavModules')).toBe('{}')
      expect(onApplied).toHaveBeenCalledExactlyOnceWith(
        'HeaderNavModules',
        '{}',
        false
      )
      expect(toast.error).toHaveBeenCalledOnce()
      expect(toast.error.mock.calls[0]![0]).toContain(savedMessage)
      expect(toast.error.mock.calls[0]![0]).toContain('offline')
      expect(toast.error.mock.calls[0]![0]).toContain(retryMessage)
      expect(settings.saving.value).toBe(false)
    }
  )
  it('blocks overlapping saves and refreshes only after an outstanding write', async () => {
    const settings = await setup()
    let finish!: (value: null) => void
    vi.mocked(api.put).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const saving = settings.saveOptions({ Price: 8 })
    expect(await settings.saveOptions({ Price: 9 })).toBe(false)
    const refresh = settings.load(true)
    expect(api.get).toHaveBeenCalledTimes(1)
    vi.mocked(api.get).mockResolvedValueOnce([{ key: 'Price', value: '8' }])
    finish(null)
    expect(await saving).toBe(true)
    expect(await refresh).toBe(true)
    expect(settings.rawValue('Price', 7.3)).toBe(8)
  })
  it('surfaces an unavailable response and retries without treating defaults as fetched', async () => {
    const { useSystemSettings } = await import('../useSystemSettings')
    const settings = useSystemSettings()
    vi.mocked(api.get).mockRejectedValueOnce(new Error('unavailable'))
    expect(await settings.load()).toBe(false)
    expect(settings.loaded.value).toBe(false)
    vi.mocked(api.get).mockResolvedValueOnce([])
    expect(await settings.load()).toBe(true)
    expect(await settings.load()).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(2)
  })
})
