import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'

import HeroSignalConsole from '@/components/home/hero/HeroSignalConsole.vue'
import {
  INITIAL_SIGNAL_SNAPSHOT,
  type SignalConsoleSnapshot,
} from '@/constants/home/signalConsole'
import i18n, { setLocale } from '@/i18n'

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

  // Tabbing away and back while the mouse still rests on the head is a new
  // keyboard visit; the earlier close must not keep the drawer shut.
  it('re-arms the hold when keyboard focus returns under a resting pointer', async () => {
    wrapper = mountConsole()
    const { head, trigger } = parts(wrapper)

    await head.trigger('pointerenter')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    await trigger.trigger('click')
    ;(trigger.element as HTMLElement).blur()
    await vi.advanceTimersByTimeAsync(0)

    ;(trigger.element as HTMLElement).focus()
    await vi.advanceTimersByTimeAsync(HOLD_MS)

    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  // The drawer scrolls on its own, so keyboard users tab into it; focus
  // leaving the trigger for the drawer is still focus within the inspector.
  it('stays open while keyboard focus moves into the drawer', async () => {
    wrapper = mountConsole()
    const { trigger } = parts(wrapper)

    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    const drawer = wrapper.get('#signal-trace-drawer')
    expect(drawer.attributes('tabindex')).toBe('0')

    ;(drawer.element as HTMLElement).focus()
    await vi.advanceTimersByTimeAsync(HOLD_MS)

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(drawer.element)
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

const ROUTED: SignalConsoleSnapshot = {
  ...INITIAL_SIGNAL_SNAPSHOT,
  phase: 'route',
  averageLatencyMs: 142.4,
  traces: [
    {
      id: 'auto-1',
      origin: 'auto',
      phase: 'route',
      startedAt: 0,
      phaseStartedAt: 0,
      sources: [
        { id: 's1', label: 'NA-CENTRAL', country: 'US' },
        { id: 's2', label: 'EU-CENTRAL', country: 'DE' },
      ],
      sourceCount: 2,
      hubLabel: 'MODEL GATEWAY',
      hops: [{ modelId: 'claude', modelName: 'Claude', state: 'outbound' }],
      latencyMs: 118,
    },
  ],
}

// The visual telemetry is aria-hidden English; what assistive technology
// reads has to follow the page language without English fragments.
describe('HeroSignalConsole screen-reader text', () => {
  afterEach(() => setLocale('zh-CN'))

  function readable(wrapper: VueWrapper) {
    return {
      status: wrapper.get('aside > p.sr-only').text(),
      origin: wrapper.get('.signal-console__origin .sr-only').text(),
      meter: wrapper.get('[role="img"]').attributes('aria-label'),
    }
  }

  it('reads the routed request in Chinese', async () => {
    await setLocale('zh-CN')
    const wrapper = mount(HeroSignalConsole, {
      props: { snapshot: ROUTED },
      global: { plugins: [i18n] },
    })

    expect(readable(wrapper)).toEqual({
      status:
        '路由演示：NA-CENTRAL和EU-CENTRAL的请求经网关路由至 Claude，转发中，延迟 118 毫秒。',
      origin: '当前请求来源国家：US和DE',
      meter: '平均请求延迟 142 毫秒',
    })
    wrapper.unmount()
  })

  it('reads the routed request in English', async () => {
    await setLocale('en')
    const wrapper = mount(HeroSignalConsole, {
      props: { snapshot: ROUTED },
      global: { plugins: [i18n] },
    })

    expect(readable(wrapper)).toEqual({
      status:
        'Routing demo: Request from NA-CENTRAL and EU-CENTRAL routed through the gateway to Claude, routing, latency 118 milliseconds.',
      origin: 'Current request countries: US and DE',
      meter: 'Average request latency 142 milliseconds',
    })
    wrapper.unmount()
  })
})
