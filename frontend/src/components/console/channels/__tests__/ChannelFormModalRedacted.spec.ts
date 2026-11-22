import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi, type Mock } from 'vitest'

import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import enConsole from '@/i18n/locales/en/console'
import { parseChannelStatusDetails } from '@/api/channelStatus'
import { HIDDEN_FIELD_CLEAR_VALUE } from '@/constants/adminChannels'
import channelSecurity from '../../../../../tests/fixtures/native/channel-security.json'
import type { AdminChannel, AdminChannelUpdateInput } from '@/types/console'

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

// Produced by the backend handlers (controller frontend contract tests).
const channel: AdminChannel = parseChannelStatusDetails(
  channelSecurity.redacted_channel.body.data
)

const paramHidden: AdminChannel = parseChannelStatusDetails(
  channelSecurity.redacted_param_override.body.data
)

const overrides = [
  {
    field: 'header_override',
    label: 'Header overrides JSON',
    name: 'header overrides',
    replacement: '{"X-Upstream":"new"}',
  },
  {
    field: 'param_override',
    label: 'Parameter overrides JSON',
    name: 'parameter overrides',
    replacement: '{"temperature":0.2}',
  },
] as const

afterEach(() => {
  document.body.innerHTML = ''
})

type Save = Mock<(input: AdminChannelUpdateInput) => Promise<boolean>>

function newSave(): Save {
  return vi
    .fn<(input: AdminChannelUpdateInput) => Promise<boolean>>()
    .mockResolvedValue(true)
}

function mountForm(save: Save, editing: AdminChannel = channel) {
  return mount(ChannelFormModal, {
    attachTo: document.body,
    props: { open: true, editing, save, fetchModels: vi.fn() },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: enConsole },
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
}

async function openAdvanced() {
  ;[...document.body.querySelectorAll('button')]
    .find((item) => item.textContent?.includes('Advanced settings'))!
    .click()
  await flushPromises()
}

async function saveForm() {
  ;[...document.body.querySelectorAll('button')]
    .find((item) => item.textContent?.includes('Save channel'))!
    .click()
  await flushPromises()
}

describe('redacted channel fields', () => {
  it('keeps hidden fields out of an unchanged save and hides their stand-in controls', async () => {
    const save = newSave()
    const wrapper = mountForm(save)
    await flushPromises()
    await openAdvanced()

    const header = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Header overrides JSON"]'
    )!
    expect(header.value).toBe('')
    expect(document.body.textContent).toContain(
      'Hidden. The stored value is kept unless you enter a replacement.'
    )
    expect(document.body.textContent).toContain(
      'The proxy, plugin and pass-through settings contain hidden credentials'
    )
    expect(document.body.textContent).toContain(
      'The advanced route and plugin configuration settings contain hidden credentials'
    )
    expect(
      document.body.querySelector(
        '[role="switch"][aria-label="Pass through request body"]'
      )
    ).toBeNull()
    expect(document.body.textContent).not.toContain('__ren2hub_redacted__')

    await saveForm()
    expect(save).toHaveBeenCalledOnce()
    const payload = save.mock.calls[0]![0]
    expect(payload).not.toHaveProperty('header_override')
    expect(payload).not.toHaveProperty('setting')
    expect(payload).not.toHaveProperty('settings')
    wrapper.unmount()
  })

  it.each(overrides)(
    'replaces a hidden $field as a whole',
    async ({ field, label, replacement }) => {
      const save = newSave()
      const wrapper = mountForm(save, paramHidden)
      await flushPromises()
      await openAdvanced()

      const input = document.body.querySelector<HTMLTextAreaElement>(
        `textarea[aria-label="${label}"]`
      )!
      expect(input.value).toBe('')
      input.value = replacement
      input.dispatchEvent(new Event('input'))
      await flushPromises()
      await saveForm()

      const payload = save.mock.calls[0]![0]
      expect(payload[field]).toBe(replacement)
      for (const other of overrides)
        if (other.field !== field)
          expect(payload).not.toHaveProperty(other.field)
      wrapper.unmount()
    }
  )

  it.each(overrides)(
    'clears a hidden $field only after confirmation',
    async ({ field, name }) => {
      const save = newSave()
      const wrapper = mountForm(save, paramHidden)
      await flushPromises()
      await openAdvanced()
      // The confirmation renders after the form, so the last match is its button.
      const button = (label: string) =>
        [...document.body.querySelectorAll('button')]
          .filter((item) => item.textContent?.trim() === label)
          .at(-1)!

      button(`Clear ${name}`).click()
      await flushPromises()
      expect(document.body.textContent).toContain(
        `The hidden ${name} will be removed when you save the channel.`
      )
      button('common.cancel').click()
      await flushPromises()
      await saveForm()
      expect(save.mock.calls[0]![0]).not.toHaveProperty(field)

      button(`Clear ${name}`).click()
      await flushPromises()
      button('Clear').click()
      await flushPromises()
      expect(document.body.textContent).toContain(
        `The ${name} will be cleared when you save.`
      )
      await saveForm()
      expect(save.mock.calls[1]![0][field]).toBe(HIDDEN_FIELD_CLEAR_VALUE)

      // Keeping the stored value undoes the pending clear.
      button('Keep stored value').click()
      await flushPromises()
      await saveForm()
      expect(save.mock.calls[2]![0]).not.toHaveProperty(field)
      wrapper.unmount()
    }
  )
})
