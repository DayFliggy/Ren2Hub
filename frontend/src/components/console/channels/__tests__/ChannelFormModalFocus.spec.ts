import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'

vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: true,
    hasPermission: () => true,
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
  name: 'Focus contract',
  type: 1,
  channel_ratio: '1',
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
  models: 'gpt-test',
  model_mapping: '',
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('async fetched models focus', () => {
  it.each(['body', 'another-control', 'closed'] as const)(
    'restores only lost focus after fetch failure with focus state %s',
    async (focusState) => {
      let rejectFetch!: (error: Error) => void
      const fetchModels = vi.fn(
        () =>
          new Promise<string[]>((_resolve, reject) => (rejectFetch = reject))
      )
      const wrapper = mount(ChannelFormModal, {
        attachTo: document.body,
        props: {
          open: true,
          editing: channel,
          save: vi.fn().mockResolvedValue(true),
          fetchModels,
        },
        global: {
          stubs: { transition: true },
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
      try {
        await flushPromises()
        const trigger = Array.from(
          document.querySelectorAll<HTMLButtonElement>(
            '.channel-form-quick-action'
          )
        ).find((button) =>
          button.textContent?.includes('channels.quickFetchUpstream')
        )!
        trigger.focus()
        trigger.click()
        await flushPromises()
        expect(trigger.disabled).toBe(true)
        document.body.tabIndex = -1
        document.body.focus()
        document.body.removeAttribute('tabindex')
        const name = document.querySelector<HTMLInputElement>(
          '[name="admin-channel-name"]'
        )!
        if (focusState === 'another-control') name.focus()
        if (focusState === 'closed') await wrapper.setProps({ open: false })
        rejectFetch(new Error('Synthetic upstream unavailable'))
        await flushPromises()
        expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(
          focusState === 'closed' ? 0 : 1
        )
        expect(document.activeElement).toBe(
          focusState === 'body'
            ? trigger
            : focusState === 'another-control'
              ? name
              : document.body
        )
        if (focusState !== 'closed') expect(trigger.disabled).toBe(false)
      } finally {
        wrapper.unmount()
      }
    }
  )

  it.each(['escape', 'apply'] as const)(
    'returns focus to the fetch control after %s despite its loading disabled state',
    async (action) => {
      let resolveFetch!: (models: string[]) => void
      const fetchModels = vi.fn(
        () => new Promise<string[]>((resolve) => (resolveFetch = resolve))
      )
      const wrapper = mount(ChannelFormModal, {
        attachTo: document.body,
        props: {
          open: true,
          editing: channel,
          save: vi.fn().mockResolvedValue(true),
          fetchModels,
        },
        global: {
          stubs: { transition: true },
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
      try {
        await flushPromises()
        const trigger = Array.from(
          document.querySelectorAll<HTMLButtonElement>(
            '.channel-form-quick-action'
          )
        ).find((button) =>
          button.textContent?.includes('channels.quickFetchUpstream')
        )!
        trigger.focus()
        trigger.click()
        await flushPromises()
        expect(trigger.disabled).toBe(true)
        // Browsers blur a focused native button when loading disables it.
        // The DOM test environment does not implement that default action.
        document.body.tabIndex = -1
        document.body.focus()
        document.body.removeAttribute('tabindex')
        expect(document.activeElement).toBe(document.body)
        resolveFetch(['gpt-test', 'gpt-new'])
        await flushPromises()
        expect(fetchModels).toHaveBeenCalledExactlyOnceWith({ channelId: 7 })
        expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(2)
        if (action === 'escape')
          window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
        else
          document
            .querySelector<HTMLButtonElement>('[data-apply-fetched-models]')!
            .click()
        await flushPromises()
        expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
        expect(document.activeElement).toBe(trigger)
      } finally {
        wrapper.unmount()
      }
    }
  )
})
