import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import ConsoleNavScroller from '@/components/console/ConsoleNavScroller.vue'

describe('mobile navigation font loading', () => {
  let finishFonts: () => void
  let itemLeft: number
  let itemWidth: number
  let stripWidth: number
  let scrollPosition: number
  let writes: number
  let wrapper: VueWrapper | undefined
  const fonts = Object.getOwnPropertyDescriptor(document, 'fonts')!
  const scrollLeft = Object.getOwnPropertyDescriptor(
    Element.prototype,
    'scrollLeft'
  )!

  beforeEach(() => {
    itemLeft = 1021
    itemWidth = 76
    stripWidth = 1189
    scrollPosition = 0
    writes = 0
    Object.defineProperty(document, 'fonts', {
      configurable: true,
      value: {
        ready: new Promise<void>((resolve) => {
          finishFonts = resolve
        }),
      },
    })
    vi.spyOn(Element.prototype, 'clientWidth', 'get').mockImplementation(
      function (this: Element) {
        return this.classList.contains('nav-scroller') ? 375 : 0
      }
    )
    vi.spyOn(HTMLElement.prototype, 'offsetLeft', 'get').mockImplementation(
      function (this: HTMLElement) {
        return this.getAttribute('aria-current') === 'page' ? itemLeft : 0
      }
    )
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(
      function (this: HTMLElement) {
        return this.getAttribute('aria-current') === 'page' ? itemWidth : 0
      }
    )
    Object.defineProperty(Element.prototype, 'scrollLeft', {
      configurable: true,
      get: () => scrollPosition,
      set: (position: number) => {
        scrollPosition = Math.max(0, Math.min(position, stripWidth - 375))
        writes++
      },
    })
    wrapper = mount(ConsoleNavScroller, {
      props: { activeKey: 'settings', fadeSurface: '#fff' },
      slots: { default: '<a aria-current="page">Settings</a>' },
    })
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.restoreAllMocks()
    Object.defineProperty(document, 'fonts', fonts)
    Object.defineProperty(Element.prototype, 'scrollLeft', scrollLeft)
  })

  async function loadWiderFont() {
    itemLeft = 1244
    itemWidth = 95
    stripWidth = 1450
    finishFonts()
    await flushPromises()
  }

  it('reveals the current item after font widths change without a container resize', async () => {
    expect(scrollPosition).toBe(814)
    await loadWiderFont()
    expect(scrollPosition).toBe(1075)
    expect(itemLeft - scrollPosition).toBeGreaterThanOrEqual(0)
    expect(itemLeft + itemWidth - scrollPosition).toBeLessThanOrEqual(375)
  })

  it.each(['pointerdown', 'wheel', 'keydown'])(
    'preserves the user position after %s while fonts are loading',
    async (event) => {
      await wrapper!.get('.nav-scroller').trigger(event)
      scrollPosition = 20
      await loadWiderFont()
      expect(scrollPosition).toBe(20)
    }
  )

  it('does not scroll after the component unmounts', async () => {
    wrapper!.unmount()
    wrapper = undefined
    const previousWrites = writes
    await loadWiderFont()
    expect(writes).toBe(previousWrites)
  })

  it('still centers a newly selected page after a manual interaction', async () => {
    await wrapper!.get('.nav-scroller').trigger('wheel')
    await loadWiderFont()
    scrollPosition = 20
    await wrapper!.setProps({ activeKey: 'users' })
    expect(scrollPosition).toBe(1075)
  })
})
