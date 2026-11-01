import { afterEach, describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'

import IntegrationMarquee from '@/components/home/hero/IntegrationMarquee.vue'
import i18n from '@/i18n'

describe('IntegrationMarquee', () => {
  let wrapper: VueWrapper | undefined

  afterEach(() => wrapper?.unmount())

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
