import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

import CommandPaletteModal from '../CommandPaletteModal.vue'

const push = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: false, hasPermission: () => false }),
}))

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
