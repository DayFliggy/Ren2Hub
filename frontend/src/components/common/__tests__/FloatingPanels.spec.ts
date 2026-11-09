import { h, nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

import ChipPicker from '@/components/common/ChipPicker.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import FieldVisibilityMenu from '@/components/common/FieldVisibilityMenu.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import MultiFilterSelect from '@/components/common/MultiFilterSelect.vue'
import ToastHost from '@/components/common/ToastHost.vue'
import { useToast } from '@/composables/useToast'
import i18n from '@/i18n'
import { lockBodyScroll, unlockBodyScroll } from '@/utils/modalStack'

const options = [
  { value: 'a', label: 'Alpha' },
  { value: 'b', label: 'Beta' },
]

/** jsdom has no layout: the anchor reports `anchor`, the panel reports its
    natural size through offsetWidth and scrollHeight. */
function stubLayout(
  anchor: Partial<DOMRect>,
  panel: { width: number; height: number }
) {
  const isPanel = (element: Element) => element.matches('[data-floating-panel]')
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(
    function (this: Element) {
      return {
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        width: 0,
        height: 0,
        x: 0,
        y: 0,
        toJSON: () => ({}),
        ...anchor,
      } as DOMRect
    }
  )
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(
    function (this: HTMLElement) {
      return isPanel(this) ? panel.width : 0
    }
  )
  vi.spyOn(Element.prototype, 'scrollHeight', 'get').mockImplementation(
    function (this: Element) {
      return isPanel(this) ? panel.height : 0
    }
  )
}

function tapOn(target: Element) {
  target.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
  target.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

enableAutoUnmount(afterEach)
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('floating panels', () => {
  it('teleports the select panel into the viewport and flips it above a low trigger', async () => {
    vi.stubGlobal('innerWidth', 390)
    vi.stubGlobal('innerHeight', 844)
    stubLayout(
      { top: 780, bottom: 824, left: 300, right: 420, width: 120, height: 44 },
      { width: 120, height: 200 }
    )
    const wrapper = mount(FilterSelect, {
      props: { modelValue: 'a', options, label: 'Status' },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    await wrapper.get('[role="combobox"]').trigger('click')
    await nextTick()
    await nextTick()

    const panel = document.querySelector<HTMLElement>('[role="listbox"]')!
    expect(panel.parentElement).toBe(document.body)
    expect(panel.style.position).toBe('fixed')
    // Opens upward (780 - 8 - 200) and stays 8px inside the right edge.
    expect(panel.style.top).toBe('572px')
    expect(panel.style.left).toBe('262px')
    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('treats a tap inside the teleported select panel as inside the control', async () => {
    const wrapper = mount(FilterSelect, {
      props: { modelValue: 'a', options, label: 'Status' },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    const trigger = wrapper.get('[role="combobox"]')
    await trigger.trigger('click')
    const option = document.querySelectorAll('[role="option"]')[1]!
    tapOn(option)
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['b'])

    await trigger.trigger('click')
    // onClickOutside drops a second click in the same macrotask.
    await new Promise((resolve) => setTimeout(resolve))
    tapOn(document.body)
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })

  it('keeps ChipPicker selection on pointerdown inside its teleported panel', async () => {
    const wrapper = mount(ChipPicker, {
      props: { options: ['gpt-5', 'claude'], modelValue: [] },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    await wrapper.get('input').trigger('focus')
    const panel = document.querySelector('[data-handdrawn="menu"]')!
    expect(wrapper.element.contains(panel)).toBe(false)
    const choice = panel.querySelector('button')!
    const down = new PointerEvent('pointerdown', {
      bubbles: true,
      cancelable: true,
    })
    choice.dispatchEvent(down)
    // Prevented so the input keeps focus on touch devices.
    expect(down.defaultPrevented).toBe(true)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['gpt-5']])
    expect(document.querySelector('[data-handdrawn="menu"]')).not.toBeNull()
    wrapper.unmount()
  })

  it('closes the date popover with Escape from inside the teleported panel', async () => {
    const wrapper = mount(DateRangePicker, {
      props: { start: '2026-07-15', end: '' },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    const trigger = wrapper.get('button[aria-haspopup="dialog"]')
    await trigger.trigger('click')
    await nextTick()
    const panel = document.querySelector('[role="dialog"]')!
    expect(wrapper.element.contains(panel)).toBe(false)
    await new Promise((resolve) => setTimeout(resolve))
    tapOn(panel)
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('true')

    ;(document.activeElement as HTMLElement).dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await nextTick()
    await nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it('returns focus to the field menu trigger when Tab leaves its panel', async () => {
    const wrapper = mount(FieldVisibilityMenu, {
      props: {
        modelValue: ['a'],
        allFields: ['a', 'b'],
        defaultFields: ['a'],
        labelFor: (field: string) => field,
        title: 'Fields',
        resetLabel: 'Reset',
      },
      attachTo: document.body,
    })
    const trigger = wrapper.get('button[aria-haspopup="dialog"]')
    await trigger.trigger('click')
    await nextTick()
    const buttons = document.querySelectorAll<HTMLButtonElement>(
      '[role="dialog"] button'
    )
    const last = buttons[buttons.length - 1]!
    last.focus()
    const tab = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true,
    })
    last.dispatchEvent(tab)
    await nextTick()
    await nextTick()
    expect(tab.defaultPrevented).toBe(true)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it('skips display:none controls when Tab leaves the date popover', async () => {
    const wrapper = mount(DateRangePicker, {
      props: { start: '2026-07-15', end: '' },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    const trigger = wrapper.get('button[aria-haspopup="dialog"]')
    await trigger.trigger('click')
    await nextTick()
    const panel = document.querySelector<HTMLElement>('[role="dialog"]')!
    // The preset rail is hidden on phones; jsdom has no layout, so emulate
    // the hidden column the way the browser reports it.
    const rail = panel.querySelector<HTMLElement>('.sm\\:flex')!
    vi.spyOn(HTMLElement.prototype, 'offsetParent', 'get').mockImplementation(
      function (this: HTMLElement) {
        return rail.contains(this) ? null : this.parentElement
      }
    )
    const prev = panel.querySelector<HTMLButtonElement>(
      `button[aria-label="${i18n.global.t('logs.prevMonth')}"]`
    )!
    prev.focus()
    const shiftTab = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true,
    })
    prev.dispatchEvent(shiftTab)
    await nextTick()
    await nextTick()
    expect(shiftTab.defaultPrevented).toBe(true)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it('lets a picker inside a dialog own Tab and Escape in its teleported panel', async () => {
    const wrapper = mount(ConsoleModal, {
      props: { open: true, title: 'Filters' },
      slots: {
        default: () => [
          h('button', 'First'),
          h(DateRangePicker, { start: '2026-07-15', end: '' }),
        ],
      },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    await nextTick()
    const trigger = document.querySelector<HTMLButtonElement>(
      'button[aria-haspopup="dialog"]'
    )!
    trigger.click()
    await flushPromises()
    const day = document.activeElement as HTMLElement
    expect(day.dataset.dateKey).toBe('2026-07-15')
    const shiftTab = new KeyboardEvent('keydown', {
      key: 'Tab',
      shiftKey: true,
      bubbles: true,
      cancelable: true,
    })
    day.dispatchEvent(shiftTab)
    // Moving back within the panel is native focus order, not the trap's.
    expect(shiftTab.defaultPrevented).toBe(false)
    expect(document.activeElement).toBe(day)

    day.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await nextTick()
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it('keeps focus on the multi-select trigger so Escape closes only the panel inside a dialog', async () => {
    const wrapper = mount(ConsoleModal, {
      props: { open: true, title: 'Filters' },
      slots: {
        default: () =>
          h(MultiFilterSelect, {
            modelValue: [],
            options,
            label: 'Kinds',
            placeholder: 'All',
          }),
      },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    await nextTick()
    const trigger = document.querySelector<HTMLElement>('[role="combobox"]')!
    trigger.focus()
    trigger.click()
    await nextTick()
    const option = document.querySelector<HTMLElement>('[role="option"]')!
    const down = new MouseEvent('mousedown', {
      bubbles: true,
      cancelable: true,
    })
    option.dispatchEvent(down)
    // Prevented mousedown is what keeps the browser from moving focus away.
    expect(down.defaultPrevented).toBe(true)
    option.click()
    await nextTick()
    expect(trigger.getAttribute('aria-expanded')).toBe('true')

    trigger.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      })
    )
    await nextTick()
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it('moves ChipPicker focus into its options and back, without closing on the way', async () => {
    const wrapper = mount(ChipPicker, {
      props: { options: ['gpt-5', 'claude', 'gemini'], modelValue: [] },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    vi.useFakeTimers()
    try {
      const input = wrapper.get('input')
      input.element.focus()
      await input.trigger('focus')
      const tab = new KeyboardEvent('keydown', {
        key: 'Tab',
        bubbles: true,
        cancelable: true,
      })
      input.element.dispatchEvent(tab)
      expect(tab.defaultPrevented).toBe(true)
      const first = document.activeElement as HTMLButtonElement
      expect(first.textContent?.trim()).toBe('gpt-5')
      // The input's blur timer must not close a panel that holds focus.
      await input.trigger('blur')
      vi.advanceTimersByTime(200)
      await nextTick()
      expect(document.querySelector('[data-floating-panel]')).not.toBeNull()

      first.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true })
      )
      const second = document.activeElement as HTMLButtonElement
      expect(second.textContent?.trim()).toBe('claude')
      second.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
      )
      await nextTick()
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['claude']])
      const escape = new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      })
      ;(document.activeElement as HTMLElement).dispatchEvent(escape)
      await nextTick()
      expect(escape.defaultPrevented).toBe(true)
      expect(document.activeElement).toBe(input.element)
      expect(document.querySelector('[data-floating-panel]')).toBeNull()
    } finally {
      vi.useRealTimers()
      wrapper.unmount()
    }
  })

  it('renders toasts outside the inert app root while an overlay is open', async () => {
    document.body.innerHTML = '<div id="app"></div>'
    const wrapper = mount(ToastHost, {
      global: { plugins: [i18n] },
      attachTo: document.getElementById('app')!,
    })
    const holder = Symbol('dialog')
    lockBodyScroll(holder)
    try {
      useToast().error('Saved failed')
      await nextTick()
      const alert = document.querySelector('[role="alert"]')!
      expect(document.getElementById('app')!.inert).toBe(true)
      expect(alert.closest('#app')).toBeNull()
      alert.querySelector('button')!.click()
      expect(useToast().toasts).toHaveLength(0)
    } finally {
      unlockBodyScroll(holder)
      wrapper.unmount()
    }
  })
})
