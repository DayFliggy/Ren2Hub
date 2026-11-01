import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'

import HeroSignalConsole from '@/components/home/hero/HeroSignalConsole.vue'
import { INITIAL_SIGNAL_SNAPSHOT } from '@/constants/home/signalConsole'
import i18n from '@/i18n'

const HOLD_MS = 3000

function mountConsole() {
  return mount(HeroSignalConsole, {
    props: { snapshot: { ...INITIAL_SIGNAL_SNAPSHOT } },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

function parts(wrapper: VueWrapper) {
  return {
    head: wrapper.get('.signal-console__telemetry-head'),
    trigger: wrapper.get('.signal-console__telemetry-trigger'),
  }
}

describe('HeroSignalConsole trace drawer', () => {
  let wrapper: VueWrapper | undefined

  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    wrapper?.unmount()
    vi.useRealTimers()
  })

  // A click leaves focus on the trigger, so leaving with the pointer used to
  // restart the hover hold and reopen the drawer 3s after the user closed it.
  it.each(['click', 'Escape'])(
    'stays closed after a %s close while the trigger keeps focus',
    async (closeWith) => {
      wrapper = mountConsole()
      const { head, trigger } = parts(wrapper)

      await head.trigger('pointerenter')
      ;(trigger.element as HTMLElement).focus()
      await trigger.trigger('click')
      expect(trigger.attributes('aria-expanded')).toBe('true')

      if (closeWith === 'click') await trigger.trigger('click')
      else await trigger.trigger('keydown', { key: 'Escape' })
      await head.trigger('pointerleave')
      await vi.advanceTimersByTimeAsync(HOLD_MS + 400)

      expect(trigger.attributes('aria-expanded')).toBe('false')
      expect(document.activeElement).toBe(trigger.element)
    }
  )

  it('re-arms the hover hold when the pointer comes back', async () => {
    wrapper = mountConsole()
    const { head, trigger } = parts(wrapper)

    await head.trigger('pointerenter')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    await trigger.trigger('click')
    await head.trigger('pointerleave')
    await vi.advanceTimersByTimeAsync(HOLD_MS)
    expect(trigger.attributes('aria-expanded')).toBe('false')

    await head.trigger('pointerenter')
    await vi.advanceTimersByTimeAsync(HOLD_MS)

    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  it('re-arms the hover hold once pointer and focus have left', async () => {
    wrapper = mountConsole()
    const { head, trigger } = parts(wrapper)

    await head.trigger('pointerenter')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    await trigger.trigger('click')
    await head.trigger('pointerleave')
    ;(trigger.element as HTMLElement).blur()
    await vi.advanceTimersByTimeAsync(0)

    await head.trigger('pointerenter')
    await vi.advanceTimersByTimeAsync(HOLD_MS)

    expect(trigger.attributes('aria-expanded')).toBe('true')
  })
})
