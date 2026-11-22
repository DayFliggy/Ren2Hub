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

afterEach(() => {
  document.body.innerHTML = ''
})

type Save = Mock<(input: AdminChannelUpdateInput) => Promise<boolean>>

function newSave(): Save {
  return vi
    .fn<(input: AdminChannelUpdateInput) => Promise<boolean>>()
    .mockResolvedValue(true)
}

function mountForm(save: Save) {
  return mount(ChannelFormModal, {
    attachTo: document.body,
    props: { open: true, editing: channel, save, fetchModels: vi.fn() },
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
      'The advanced route settings contain hidden credentials'
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

  it('replaces a hidden header override as a whole', async () => {
    const save = newSave()
    const wrapper = mountForm(save)
    await flushPromises()
    await openAdvanced()

    const header = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Header overrides JSON"]'
    )!
    header.value = '{"X-Upstream":"new"}'
    header.dispatchEvent(new Event('input'))
    await flushPromises()
    await saveForm()

    expect(save.mock.calls[0]![0].header_override).toBe('{"X-Upstream":"new"}')
    wrapper.unmount()
  })

  it('clears a hidden header override only after confirmation', async () => {
    const save = newSave()
    const wrapper = mountForm(save)
    await flushPromises()
    await openAdvanced()
    // The confirmation renders after the form, so the last match is its button.
    const button = (label: string) =>
      [...document.body.querySelectorAll('button')]
        .filter((item) => item.textContent?.trim() === label)
        .at(-1)!

    button('Clear header overrides').click()
    await flushPromises()
    expect(document.body.textContent).toContain(
      'The hidden header overrides will be removed when you save the channel.'
    )
    button('common.cancel').click()
    await flushPromises()
    await saveForm()
    expect(save.mock.calls[0]![0]).not.toHaveProperty('header_override')

    button('Clear header overrides').click()
    await flushPromises()
    button('Clear').click()
    await flushPromises()
    expect(document.body.textContent).toContain(
      'Header overrides will be cleared when you save.'
    )
    await saveForm()
    expect(save.mock.calls[1]![0].header_override).toBe(
      HIDDEN_FIELD_CLEAR_VALUE
    )
    wrapper.unmount()
  })
})
