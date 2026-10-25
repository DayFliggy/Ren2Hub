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
    vi.spyOn(Element.prototype, 'scrollWidth', 'get').mockImplementation(
      function (this: Element) {
        return this.classList.contains('nav-scroller') ? stripWidth : 0
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

  function applyNarrowerFont() {
    itemLeft = 650
    itemWidth = 60
    stripWidth = 900
    // Font layout can clamp the browser's position without a JS assignment.
    scrollPosition = Math.min(scrollPosition, stripWidth - 375)
  }

  it.each([true, false])(
    'recenters after native font-width clamping (scroll delivered: %s)',
    async (delivered) => {
      expect(scrollPosition).toBe(814)
      applyNarrowerFont()
      expect(scrollPosition).toBe(525)
      if (delivered) await wrapper!.get('.nav-scroller').trigger('scroll')
      finishFonts()
      await flushPromises()
      expect(scrollPosition).toBe(492.5)
      expect(itemLeft + itemWidth / 2 - scrollPosition).toBe(375 / 2)
    }
  )

  it.each([true, false])(
    'preserves a user position during font narrowing (scroll delivered: %s)',
    async (delivered) => {
      scrollPosition = 20
      if (delivered) await wrapper!.get('.nav-scroller').trigger('scroll')
      applyNarrowerFont()
      finishFonts()
      await flushPromises()
      expect(scrollPosition).toBe(20)
    }
  )

  it('preserves an established manual position even when fonts clamp it', async () => {
    scrollPosition = 700
    await wrapper!.get('.nav-scroller').trigger('scroll')
    applyNarrowerFont()
    expect(scrollPosition).toBe(525)
    await wrapper!.get('.nav-scroller').trigger('scroll')
    finishFonts()
    await flushPromises()
    expect(scrollPosition).toBe(525)
  })

  it('reveals the current item after font widths change without a container resize', async () => {
    expect(scrollPosition).toBe(814)
    await loadWiderFont()
    expect(scrollPosition).toBe(1075)
    expect(itemLeft - scrollPosition).toBeGreaterThanOrEqual(0)
    expect(itemLeft + itemWidth - scrollPosition).toBeLessThanOrEqual(375)
  })

  it.each([
    { event: 'pointerdown', options: {} },
    { event: 'wheel', options: { deltaY: 100, deltaX: 0 } },
    { event: 'keydown', options: { key: 'Enter' } },
    { event: 'keydown', options: { key: 'Tab' } },
  ])(
    'still corrects font widths after $event $options without horizontal movement',
    async ({ event, options }) => {
      await wrapper!.get('.nav-scroller').trigger(event, options)
      await loadWiderFont()
      expect(itemLeft - scrollPosition).toBeGreaterThanOrEqual(0)
      expect(itemLeft + itemWidth - scrollPosition).toBeLessThanOrEqual(375)
    }
  )

  it('preserves actual horizontal scrolling while fonts are loading', async () => {
    scrollPosition = 20
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await loadWiderFont()
    expect(scrollPosition).toBe(20)
  })

  it('preserves a changed position before its scroll event is delivered', async () => {
    scrollPosition = 20
    await loadWiderFont()
    expect(scrollPosition).toBe(20)
  })

  it('does not treat its own centering scroll event as manual scrolling', async () => {
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await loadWiderFont()
    expect(itemLeft + itemWidth - scrollPosition).toBeLessThanOrEqual(375)
  })

  it('does not scroll after the component unmounts', async () => {
    wrapper!.unmount()
    wrapper = undefined
    const previousWrites = writes
    await loadWiderFont()
    expect(writes).toBe(previousWrites)
  })

  it('corrects the newly selected page when fonts finish after an earlier manual scroll', async () => {
    await wrapper!.get('.nav-scroller').trigger('pointerdown')
    scrollPosition = 20
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await wrapper!.setProps({ activeKey: 'users' })
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await loadWiderFont()
    expect(scrollPosition).toBe(1075)
  })

  it('preserves a new manual scroll after selecting another page before fonts finish', async () => {
    scrollPosition = 20
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await wrapper!.setProps({ activeKey: 'users' })
    scrollPosition = 30
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await loadWiderFont()
    expect(scrollPosition).toBe(30)
  })

  it('still centers a newly selected page after fonts have finished', async () => {
    await loadWiderFont()
    scrollPosition = 20
    await wrapper!.get('.nav-scroller').trigger('scroll')
    await wrapper!.setProps({ activeKey: 'users' })
    expect(scrollPosition).toBe(1075)
  })
})
