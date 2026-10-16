import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { reactive } from 'vue'

import CommandPaletteModal from '../CommandPaletteModal.vue'

const push = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
const access = reactive({
  isAdmin: false,
  isRoot: false,
  channel: false,
  features: { user_models: 'live' } as Record<string, string>,
})
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isAdmin() {
      return access.isAdmin
    },
    get isRoot() {
      return access.isRoot
    },
    hasPermission: (resource: string) =>
      resource === 'channel' && access.channel,
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    featureStatus: (feature: string) => access.features[feature] ?? 'disabled',
  }),
}))
beforeEach(() => {
  push.mockClear()
  access.isAdmin = false
  access.isRoot = false
  access.channel = false
  access.features = { user_models: 'live' }
})

describe('command palette input', () => {
  it('does not navigate or change the active option during composition', async () => {
    vi.stubGlobal('CSS', { escape: (value: string) => value })
    const wrapper = mount(CommandPaletteModal, {
      attachTo: document.body,
      props: { open: true },
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
    try {
      await flushPromises()
      const input = document.body.querySelector<HTMLInputElement>(
        'input[name="command-search"]'
      )!
      const active = input.getAttribute('aria-activedescendant')
      for (const key of ['ArrowDown', 'ArrowUp', 'Enter', 'Escape']) {
        const event = new KeyboardEvent('keydown', {
          key,
          isComposing: true,
          bubbles: true,
          cancelable: true,
        })
        input.dispatchEvent(event)
        await flushPromises()
        expect(event.defaultPrevented).toBe(false)
        expect(input.getAttribute('aria-activedescendant')).toBe(active)
        expect(push).not.toHaveBeenCalled()
        expect(wrapper.emitted('close')).toBeUndefined()
      }
      input.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      )
      expect(push).toHaveBeenCalledWith({ name: 'models' })
      expect(wrapper.emitted('close')).toHaveLength(1)
    } finally {
      wrapper.unmount()
      vi.unstubAllGlobals()
    }
  })
})

it('reactively respects root, permissions and fail-closed capabilities while open', async () => {
  const wrapper = mount(CommandPaletteModal, {
    attachTo: document.body,
    props: { open: true },
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
  const labels = () =>
    [...document.body.querySelectorAll('[role="option"]')].map((el) =>
      el.textContent?.trim()
    )
  try {
    await flushPromises()
    expect(labels()).toEqual(['nav.models'])
    access.isAdmin = true
    access.features = {
      user_models: 'live',
      admin: 'live',
      taskPlugins: 'live',
    }
    await flushPromises()
    expect(labels()).not.toContain('nav.channelManagement')
    expect(labels()).not.toContain('nav.systemSettings')
    access.channel = true
    access.isRoot = true
    await flushPromises()
    expect(labels()).toContain('nav.channelManagement')
    expect(labels()).toContain('nav.systemSettings')
    expect(labels()).toContain('taskPlugins.title')
    access.features.taskPlugins = 'disabled'
    access.channel = false
    await flushPromises()
    expect(labels()).not.toContain('taskPlugins.title')
    expect(labels()).not.toContain('nav.channelManagement')
    access.features = {}
    await flushPromises()
    expect(labels()).toEqual([])
    expect(
      document.body
        .querySelector('input')
        ?.getAttribute('aria-activedescendant')
    ).toBeNull()
  } finally {
    wrapper.unmount()
  }
})
