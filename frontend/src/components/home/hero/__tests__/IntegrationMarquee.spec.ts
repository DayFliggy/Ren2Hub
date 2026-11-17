import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'

import IntegrationMarquee from '@/components/home/hero/IntegrationMarquee.vue'
import i18n from '@/i18n'

describe('IntegrationMarquee', () => {
  let wrapper: VueWrapper | undefined

  // jsdom has no Web Animations; the rail's motion is checked in the browser.
  beforeEach(() => {
    Object.defineProperty(Element.prototype, 'animate', {
      configurable: true,
      value: vi.fn(() => ({ currentTime: 0, playbackRate: 1, cancel() {} })),
    })
  })
  afterEach(() => {
    wrapper?.unmount()
    Reflect.deleteProperty(Element.prototype, 'animate')
  })

  // Chromium starts a native drag on the chip icons; that fires pointercancel
  // and the rail stopped following the pointer about 16px into the drag.
  it('cancels native drags that start on a chip icon', () => {
    wrapper = mount(IntegrationMarquee, {
      global: { plugins: [createPinia(), i18n] },
      attachTo: document.body,
    })
    const icon = wrapper.get('.hero-integration-marquee-wrapper img').element
    const drag = new Event('dragstart', { bubbles: true, cancelable: true })

    icon.dispatchEvent(drag)

    expect(drag.defaultPrevented).toBe(true)
  })
})
