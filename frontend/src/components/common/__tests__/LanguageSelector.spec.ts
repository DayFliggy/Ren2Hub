import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '@/i18n'
import LanguageSelector from '../LanguageSelector.vue'

describe('LanguageSelector', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('renders with 44px min-touch-height trigger and opens menu on click', async () => {
    const wrapper = mount(LanguageSelector, {
      props: { variant: 'console' },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('button[aria-haspopup="menu"]')
    expect(trigger.classes()).toContain('h-11')
    expect(trigger.classes()).toContain('min-w-11')

    expect(wrapper.find('#language-selector-menu').exists()).toBe(false)
    await trigger.trigger('click')
    await flushPromises()

    const menu = wrapper.get('#language-selector-menu')
    expect(menu.isVisible()).toBe(true)
    const options = menu.findAll('[role="menuitemradio"]')
    expect(options.length).toBeGreaterThan(0)
    for (const opt of options) {
      expect(opt.classes()).toContain('min-h-11')
    }
  })

  it('supports placement="top" for bottom drawers to open upward', async () => {
    const wrapper = mount(LanguageSelector, {
      props: { variant: 'console', placement: 'top' },
      global: { plugins: [i18n] },
    })

    await wrapper.get('button[aria-haspopup="menu"]').trigger('click')
    await flushPromises()

    const menu = wrapper.get('#language-selector-menu')
    expect(menu.classes()).toContain('bottom-full')
    expect(menu.classes()).toContain('mb-2')
    expect(menu.classes()).not.toContain('top-12')
  })

  it('defaults to bottom placement', async () => {
    const wrapper = mount(LanguageSelector, {
      props: { variant: 'console' },
      global: { plugins: [i18n] },
    })

    await wrapper.get('button[aria-haspopup="menu"]').trigger('click')
    await flushPromises()

    const menu = wrapper.get('#language-selector-menu')
    expect(menu.classes()).toContain('top-12')
    expect(menu.classes()).not.toContain('bottom-full')
  })

  it('does NOT preventDefault Escape when menu is closed, allowing parent drawer/modal to handle Escape', async () => {
    const wrapper = mount(LanguageSelector, {
      props: { variant: 'console' },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('button[aria-haspopup="menu"]')
    expect(trigger.attributes('aria-expanded')).toBe('false')

    const escapeEvent = new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    })
    trigger.element.dispatchEvent(escapeEvent)

    // defaultPrevented must be false when menu is closed so drawer/modal receives Escape
    expect(escapeEvent.defaultPrevented).toBe(false)
  })

  it('preventsDefault and closes menu on Escape when menu is open', async () => {
    const wrapper = mount(LanguageSelector, {
      props: { variant: 'console' },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })

    const trigger = wrapper.get('button[aria-haspopup="menu"]')
    await trigger.trigger('click')
    await flushPromises()

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('#language-selector-menu').exists()).toBe(true)

    const escapeEvent = new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    })
    trigger.element.dispatchEvent(escapeEvent)
    await flushPromises()

    // defaultPrevented must be true when menu is open to close menu first
    expect(escapeEvent.defaultPrevented).toBe(true)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })
})
