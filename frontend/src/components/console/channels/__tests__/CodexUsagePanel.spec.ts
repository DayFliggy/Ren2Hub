import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import CodexUsagePanel from '../CodexUsagePanel.vue'
import {
  providerUsageApi,
  type ProviderUsageSnapshot,
} from '@/api/providerUsage'
import { providerUsageMessages } from '@/i18n/providerUsage'

vi.mock('@/api/providerUsage', () => ({
  providerUsageApi: { get: vi.fn(), refresh: vi.fn(), settings: vi.fn() },
}))
const snapshot: ProviderUsageSnapshot = {
  channel_id: 71,
  provider: 'codex',
  state: 'known',
  plan_type: 'plus',
  windows: [
    {
      name: 'rate_limit_primary',
      used_percent: 0,
      limit_seconds: 18000,
      reset_at: null,
    },
  ],
  fetched_at: 100,
  expires_at: 253402300799,
  last_attempt_at: 100,
  next_refresh_at: 0,
  upstream_status: 200,
  error_code: '',
  auto_refresh: false,
}

function panel(canOperate = true, locale = 'en') {
  return mount(CodexUsagePanel, {
    props: { channelId: 71, canOperate },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale,
          messages: {
            en: { providerUsage: providerUsageMessages.en },
            'zh-CN': { providerUsage: providerUsageMessages['zh-CN'] },
          },
        }),
      ],
    },
  })
}

describe('Codex provider usage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(providerUsageApi.get).mockResolvedValue(snapshot)
    vi.mocked(providerUsageApi.refresh).mockResolvedValue(snapshot)
    vi.mocked(providerUsageApi.settings).mockResolvedValue({
      ...snapshot,
      auto_refresh: true,
    })
  })
  afterEach(() => {
    vi.useRealTimers()
  })
  it('reads cache on mount, shows explicit zero, and only fetches after an operation', async () => {
    const wrapper = panel()
    await flushPromises()
    expect(wrapper.text()).toContain('0% used')
    expect(wrapper.text()).toContain('Unknown')
    expect(providerUsageApi.refresh).not.toHaveBeenCalled()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(providerUsageApi.refresh).toHaveBeenCalledWith(
      71,
      expect.any(AbortSignal)
    )
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await flushPromises()
    expect(providerUsageApi.settings).toHaveBeenCalledWith(
      71,
      true,
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })
  it('cannot refresh or change settings without channel operation permission', async () => {
    const wrapper = panel(false)
    await flushPromises()
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('input').exists()).toBe(false)
    expect(wrapper.text()).toContain('Automatic collection is off')
    expect(providerUsageApi.refresh).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('shows unknown and failed historical data without turning either into fresh zero', async () => {
    vi.mocked(providerUsageApi.get).mockResolvedValue({
      ...snapshot,
      state: 'unknown',
      windows: [],
    })
    const wrapper = panel(true, 'zh-CN')
    await flushPromises()
    expect(wrapper.text()).toContain('尚未采集用量')
    expect(wrapper.text()).not.toContain('已用 0%')
    vi.mocked(providerUsageApi.refresh).mockResolvedValue({
      ...snapshot,
      state: 'error',
      error_code: 'rate_limited',
      next_refresh_at: Math.floor(Date.now() / 1000) + 900,
    })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('刷新失败')
    expect(wrapper.text()).toContain('供应商限制了刷新频率')
    expect((wrapper.get('button').element as HTMLButtonElement).disabled).toBe(
      true
    )
    wrapper.unmount()
  })
  it('aborts old channel reads and ignores late results', async () => {
    let finish!: (value: ProviderUsageSnapshot) => void
    vi.mocked(providerUsageApi.get).mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = panel()
    const oldSignal = vi.mocked(providerUsageApi.get).mock.calls[0]![1]!
    vi.mocked(providerUsageApi.get).mockResolvedValue({
      ...snapshot,
      channel_id: 72,
      state: 'unknown',
      windows: [],
    })
    await wrapper.setProps({ channelId: 72 })
    await flushPromises()
    finish(snapshot)
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.text()).not.toContain('0% used')
    expect(wrapper.text()).toContain('No usage has been collected')
    wrapper.unmount()
  })
  it('updates expiry and cooldown locally without an automatic network fetch', async () => {
    vi.useFakeTimers()
    const now = Math.floor(Date.now() / 1000)
    vi.mocked(providerUsageApi.get).mockResolvedValue({
      ...snapshot,
      expires_at: now + 1,
      next_refresh_at: now + 1,
    })
    const wrapper = panel()
    await flushPromises()
    expect((wrapper.get('button').element as HTMLButtonElement).disabled).toBe(
      true
    )
    await vi.advanceTimersByTimeAsync(1100)
    expect(wrapper.text()).toContain('Out of date')
    expect((wrapper.get('button').element as HTMLButtonElement).disabled).toBe(
      false
    )
    expect(providerUsageApi.get).toHaveBeenCalledTimes(1)
    expect(providerUsageApi.refresh).not.toHaveBeenCalled()
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
  it.each([false, true])(
    'restores confirmed automatic=%s after a rejected write',
    async (automatic) => {
      vi.mocked(providerUsageApi.get).mockResolvedValue({
        ...snapshot,
        auto_refresh: automatic,
      })
      vi.mocked(providerUsageApi.settings).mockRejectedValue(
        new Error('unavailable')
      )
      const wrapper = panel()
      await flushPromises()
      const checkbox = wrapper.get<HTMLInputElement>('input[type="checkbox"]')
      await checkbox.setValue(!automatic)
      await flushPromises()
      expect(checkbox.element.checked).toBe(automatic)
      expect(wrapper.get('[role="alert"]').text()).toContain('failed')
      wrapper.unmount()
    }
  )
  it('blocks repeated settings writes and ignores a late response after operation permission is revoked', async () => {
    let finish!: (value: ProviderUsageSnapshot) => void
    vi.mocked(providerUsageApi.settings).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = panel()
    await flushPromises()
    const checkbox = wrapper.get<HTMLInputElement>('input[type="checkbox"]')
    await checkbox.setValue(true)
    expect(checkbox.element.checked).toBe(false)
    await checkbox.trigger('change')
    expect(providerUsageApi.settings).toHaveBeenCalledTimes(1)
    expect(checkbox.element.disabled).toBe(true)
    const signal = vi.mocked(providerUsageApi.settings).mock.calls[0]![2]!
    await wrapper.setProps({ canOperate: false })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    finish({ ...snapshot, auto_refresh: true })
    await flushPromises()
    expect(wrapper.text()).toContain('Automatic collection is off')
    expect(wrapper.find('input').exists()).toBe(false)
    wrapper.unmount()
  })
  it('keeps an enabled setting checked until the server confirms disabling it', async () => {
    let finish!: (value: ProviderUsageSnapshot) => void
    vi.mocked(providerUsageApi.get).mockResolvedValue({
      ...snapshot,
      auto_refresh: true,
    })
    vi.mocked(providerUsageApi.settings).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = panel()
    await flushPromises()
    const checkbox = wrapper.get<HTMLInputElement>('input[type="checkbox"]')
    await checkbox.setValue(false)
    expect(checkbox.element.checked).toBe(true)
    expect(checkbox.element.disabled).toBe(true)
    finish(snapshot)
    await flushPromises()
    expect(checkbox.element.checked).toBe(false)
    wrapper.unmount()
  })
  it('aborts a pending settings write when the panel closes', async () => {
    vi.mocked(providerUsageApi.settings).mockImplementation(
      () => new Promise(() => {})
    )
    const wrapper = panel()
    await flushPromises()
    await wrapper.get('input').setValue(true)
    const signal = vi.mocked(providerUsageApi.settings).mock.calls[0]![2]!
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})
