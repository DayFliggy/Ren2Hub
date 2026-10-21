import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const put = vi.hoisted(() => vi.fn())
const toastError = vi.hoisted(() => vi.fn())

vi.mock('@/api/console', () => ({ api: { put } }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: toastError, success: vi.fn() }),
}))

import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/types/auth'
import PreferencesNotificationsPanel from '@/components/console/PreferencesNotificationsPanel.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  put.mockReset()
  put.mockResolvedValue(null)
  toastError.mockReset()
})

function render(role: number, setting: Record<string, unknown>) {
  const auth = useAuthStore()
  auth.persist({
    id: 1,
    username: 'member',
    role,
    setting: JSON.stringify(setting),
  } as UserInfo)
  vi.spyOn(auth, 'fetchSelf').mockResolvedValue(true)
  return mount(PreferencesNotificationsPanel, {
    props: { isAdmin: role >= 10 },
    global: { plugins: [i18n] },
  })
}

const toggle = (wrapper: ReturnType<typeof render>, label: string) =>
  wrapper.find(`[role="switch"][aria-label="${label}"]`)

async function save(wrapper: ReturnType<typeof render>) {
  await wrapper
    .findAll('button')
    .find((button) => button.text() === 'Save notification settings')!
    .trigger('click')
  await flushPromises()
}

describe('PreferencesNotificationsPanel', () => {
  it('saves the stored numeric threshold and keeps unset switches and the billing preference', async () => {
    const wrapper = render(1, {
      notify_type: 'email',
      quota_warning_threshold: 250000,
      channel_notify_enabled: true,
      accept_unset_model_ratio_model: true,
    })
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('input[inputmode="numeric"]').element.value
    ).toBe('250000')
    expect(
      toggle(wrapper, 'Low balance reminder').attributes('aria-checked')
    ).toBe('true')
    expect(toggle(wrapper, 'Channel notifications').exists()).toBe(false)

    await save(wrapper)

    const body = put.mock.calls[0]?.[1] as Record<string, unknown>
    expect(put.mock.calls[0]?.[0]).toBe('/api/user/setting')
    expect(body).toMatchObject({
      quota_warning_threshold: 250000,
      accept_unset_model_ratio_model: true,
    })
    expect(body).not.toHaveProperty('quota_warning_enabled')
    expect(body).not.toHaveProperty('channel_notify_enabled')
    wrapper.unmount()
  })

  it('treats an empty threshold as the site default and blocks negative values', async () => {
    const wrapper = render(1, { notify_type: 'email' })
    await flushPromises()
    const input = wrapper.get<HTMLInputElement>('input[inputmode="numeric"]')
    expect(input.element.value).toBe('')

    await save(wrapper)
    expect(put).toHaveBeenCalledOnce()
    expect(put.mock.calls[0]?.[1]).toMatchObject({ quota_warning_threshold: 0 })

    await input.setValue('-5')
    await save(wrapper)
    expect(put).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('Enter a whole quota of 0 or more')
    wrapper.unmount()
  })

  it('defaults channel notices to root only and sends switches the user changed', async () => {
    const admin = render(10, { notify_type: 'email' })
    await flushPromises()
    const channel = toggle(admin, 'Channel notifications')
    expect(channel.attributes('aria-checked')).toBe('false')
    await channel.trigger('click')
    await toggle(admin, 'Low balance reminder').trigger('click')
    await save(admin)
    expect(put.mock.calls[0]?.[1]).toMatchObject({
      channel_notify_enabled: true,
      quota_warning_enabled: false,
    })
    admin.unmount()

    setActivePinia(createPinia())
    const root = render(100, { notify_type: 'email' })
    await flushPromises()
    expect(
      toggle(root, 'Channel notifications').attributes('aria-checked')
    ).toBe('true')
    await save(root)
    expect(put.mock.calls[1]?.[1]).not.toHaveProperty('channel_notify_enabled')
    root.unmount()
  })
})
