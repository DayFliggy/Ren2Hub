import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'

import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'

vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: true, hasPermission: () => false }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    isFeatureEnabled: () => true,
    featureStatus: () => 'live',
  }),
}))

function mountForm() {
  return mount(ChannelFormModal, {
    attachTo: document.body,
    props: {
      open: true,
      editing: null,
      save: vi.fn().mockResolvedValue(true),
      fetchModels: vi.fn(),
    },
    global: {
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
}

function typeTrigger(): HTMLButtonElement {
  const trigger = document.body.querySelector<HTMLButtonElement>(
    '[role="dialog"] button[aria-expanded]'
  )
  expect(trigger).not.toBeNull()
  return trigger!
}

function pressEscape(target: Element) {
  target.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    })
  )
}

describe('ChannelFormModal type selector Escape', () => {
  it('keeps IME keys in the channel search and model input', async () => {
    const wrapper = mountForm()
    try {
      await flushPromises()
      typeTrigger().click()
      await flushPromises()
      const search = document.body.querySelector<HTMLInputElement>(
        '[role="dialog"] input[type="text"]'
      )!
      const escape = new KeyboardEvent('keydown', {
        key: 'Escape',
        isComposing: true,
        bubbles: true,
        cancelable: true,
      })
      search.dispatchEvent(escape)
      await flushPromises()
      expect(escape.defaultPrevented).toBe(false)
      expect(document.body.querySelector('[role="listbox"]')).not.toBeNull()
      expect(wrapper.emitted('close')).toBeUndefined()
      pressEscape(search)
      await flushPromises()

      const model = document.body.querySelector<HTMLInputElement>(
        'input[placeholder="channels.modelsSearchPlaceholder"]'
      )!
      expect(model).not.toBeNull()
      model.value = 'new-model'
      model.dispatchEvent(new Event('input', { bubbles: true }))
      for (const key of ['Enter', ',']) {
        const event = new KeyboardEvent('keydown', {
          key,
          isComposing: true,
          bubbles: true,
          cancelable: true,
        })
        model.dispatchEvent(event)
        await flushPromises()
        expect(event.defaultPrevented).toBe(false)
        expect(model.value).toBe('new-model')
      }
      model.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      )
      await flushPromises()
      expect(model.value).toBe('')
      expect(document.body.textContent).toContain('new-model')
    } finally {
      wrapper.unmount()
    }
  })

  it('closes only the open type list and keeps the dialog open', async () => {
    const wrapper = mountForm()
    await flushPromises()
    typeTrigger().click()
    await flushPromises()
    const search = document.body.querySelector<HTMLInputElement>(
      '[role="dialog"] input[type="text"]'
    )
    expect(search).not.toBeNull()
    expect(document.body.querySelector('[role="listbox"]')).not.toBeNull()

    pressEscape(search!)
    await flushPromises()
    expect(document.body.querySelector('[role="listbox"]')).toBeNull()
    expect(wrapper.emitted('close')).toBeUndefined()

    // With the list closed, Escape closes the dialog as before.
    pressEscape(typeTrigger())
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
