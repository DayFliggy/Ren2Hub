import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { UpstreamBinding } from '@/api/upstreams'
import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'

const permissions = vi.hoisted(() => ({ sensitive: false }))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: true,
    hasPermission: (resource: string, action: string) =>
      resource === 'channel' &&
      (action !== 'sensitive_write' || permissions.sensitive),
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    isFeatureEnabled: () => true,
    featureStatus: () => 'live',
  }),
}))

const channel: AdminChannel = {
  id: 7,
  name: 'Priced channel',
  type: 1,
  channel_ratio: '1.5',
  max_concurrency: 0,
  in_flight: 0,
  lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
  status: 1,
  priority: 0,
  weight: 1,
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: '',
  models: 'gpt-5',
  model_mapping: '',
}

function binding(patch: Partial<UpstreamBinding> = {}): UpstreamBinding {
  return {
    id: 1,
    account_id: 4,
    remote_key_id: 14,
    channel_id: channel.id,
    channel_created_time: 1799999900,
    stale: false,
    remote_status: 'active',
    ratio_sync_enabled: true,
    ratio_sync_factor: '1.25',
    ratio_sync_error: '',
    ...patch,
  }
}

function mountForm(
  save = vi.fn().mockResolvedValue(true),
  upstreamBinding: UpstreamBinding | null = null
) {
  const wrapper = mount(ChannelFormModal, {
    attachTo: document.body,
    props: {
      open: true,
      editing: channel,
      save,
      fetchModels: vi.fn(),
      upstreamBinding,
    },
    global: {
      stubs: {
        RouterLink: {
          props: ['to'],
          template: '<a :data-to="JSON.stringify(to)"><slot /></a>',
        },
      },
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  return { wrapper, save }
}
function ratioInput() {
  return document.body.querySelector<HTMLInputElement>(
    '[role="dialog"] input[name="channel-ratio"]'
  )!
}
function saveButton() {
  return [
    ...document.body.querySelectorAll<HTMLButtonElement>(
      '[role="dialog"] button'
    ),
  ].find((button) => button.textContent?.includes('channels.saveChannel'))!
}
afterEach(() => {
  permissions.sensitive = false
  document.body.innerHTML = ''
})

describe('channel multiplier permission', () => {
  it('groups numeric routing settings together and keeps their native value types', async () => {
    permissions.sensitive = true
    const { wrapper, save } = mountForm()
    try {
      await flushPromises()
      const ratio = ratioInput()
      const section = ratio.closest('.channel-form-section--advanced')
      expect(section).not.toBeNull()
      const concurrency = section!.querySelector<HTMLInputElement>(
        'input[name="channel-max-concurrency"]'
      )!
      expect(concurrency.type).toBe('number')
      expect(concurrency.min).toBe('0')
      expect(concurrency.step).toBe('1')
      expect(ratio.type).toBe('text')
      expect(ratio.inputMode).toBe('decimal')
      expect(
        ratio.parentElement?.querySelector('[aria-hidden="true"]')?.textContent
      ).toBe('×')
      const settingGrid = ratio.closest('label')!.parentElement!
      expect(settingGrid.contains(concurrency)).toBe(true)
      expect(
        settingGrid.querySelector('input[name="admin-channel-priority"]')
      ).not.toBeNull()
      expect(
        settingGrid.querySelector('input[name="admin-channel-weight"]')
      ).not.toBeNull()

      ratio.value = '0.123456789012345678'
      ratio.dispatchEvent(new Event('input'))
      concurrency.value = '4'
      concurrency.dispatchEvent(new Event('input'))
      await flushPromises()
      saveButton().click()
      await flushPromises()
      expect(save).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          channel_ratio: '0.123456789012345678',
          max_concurrency: 4,
        })
      )
    } finally {
      wrapper.unmount()
    }
  })

  it('is read-only without sensitive write and leaves the multiplier out of the update', async () => {
    const { wrapper, save } = mountForm()
    try {
      await flushPromises()
      expect(ratioInput().disabled).toBe(true)
      expect(document.body.textContent).toContain(
        'channels.channelRatioReadOnly'
      )
      saveButton().click()
      await flushPromises()
      expect(save).toHaveBeenCalledOnce()
      expect(save.mock.calls[0]![0]).not.toHaveProperty('channel_ratio')
    } finally {
      wrapper.unmount()
    }
  })
  it('stays editable for sensitive writers', async () => {
    permissions.sensitive = true
    const { wrapper } = mountForm()
    try {
      await flushPromises()
      expect(ratioInput().disabled).toBe(false)
      expect(document.body.textContent).not.toContain(
        'channels.channelRatioReadOnly'
      )
    } finally {
      wrapper.unmount()
    }
  })
})

describe('channel multiplier managed by an upstream binding', () => {
  async function submitWith(value: UpstreamBinding) {
    permissions.sensitive = true
    const { wrapper, save } = mountForm(undefined, value)
    await flushPromises()
    return { wrapper, save }
  }

  it('locks the multiplier and omits it while ratio sync is active', async () => {
    const { wrapper, save } = await submitWith(binding())
    try {
      const ratio = ratioInput()
      expect(ratio.disabled).toBe(true)
      expect(ratio.hasAttribute('data-upstream-managed')).toBe(true)
      const notice = document.body.querySelector(
        '[data-channel-upstream-notice]'
      )!
      expect(
        JSON.parse(notice.querySelector('a')!.getAttribute('data-to')!)
      ).toEqual({
        name: 'upstreams',
        query: { account: '4', tab: 'bindings' },
      })
      saveButton().click()
      await flushPromises()
      expect(save).toHaveBeenCalledOnce()
      expect(save.mock.calls[0]![0]).not.toHaveProperty('channel_ratio')
    } finally {
      wrapper.unmount()
    }
  })

  it.each([
    ['a stale binding', binding({ stale: true })],
    ['ratio sync turned off', binding({ ratio_sync_enabled: false })],
    ["another channel's binding", binding({ channel_id: 8 })],
  ])('keeps the multiplier editable and submitted for %s', async (_, value) => {
    const { wrapper, save } = await submitWith(value)
    try {
      const ratio = ratioInput()
      expect(ratio.disabled).toBe(false)
      ratio.value = '2'
      ratio.dispatchEvent(new Event('input'))
      await flushPromises()
      saveButton().click()
      await flushPromises()
      expect(save).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ channel_ratio: '2' })
      )
    } finally {
      wrapper.unmount()
    }
  })
})
