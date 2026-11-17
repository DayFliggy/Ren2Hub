import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import HeroWorldMap from '@/components/home/hero/HeroWorldMap.vue'
import i18n from '@/i18n'

const { resize, initGate } = vi.hoisted(() => ({
  resize: vi.fn(),
  initGate: { wait: Promise.resolve() },
}))

vi.mock('@/canvas/MapScene', () => ({
  MapScene: class {
    resize = resize
    init = () => initGate.wait
    start() {}
    stop() {}
    dispose() {}
    setTheme() {}
  },
}))

// jsdom has no layout, so each stage element reports a fixed rectangle.
const RECTS: Record<string, [number, number]> = {
  'data-hero-map-focus': [80, 160],
  'data-hero-map-immersive-focus': [80, 700],
  'data-hero-map-copy': [300, 400],
}

function stubRects() {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
    function (this: HTMLElement) {
      const entry = Object.entries(RECTS).find(([attr]) =>
        this.hasAttribute(attr)
      )
      const [top, height] = entry?.[1] ?? [0, 844]
      return DOMRect.fromRect({ x: 0, y: top, width: 390, height })
    }
  )
}

const Stage = defineComponent({
  props: { immersive: Boolean },
  setup(props) {
    return () =>
      h('section', [
        h(HeroWorldMap, { immersive: props.immersive }),
        ...Object.keys(RECTS).map((attr) => h('div', { [attr]: '' })),
      ])
  },
})

const lastFocusHeight = () => resize.mock.lastCall?.[0]?.height

describe('HeroWorldMap layout', () => {
  let wrapper: VueWrapper | undefined

  beforeEach(() => {
    resize.mockClear()
    initGate.wait = Promise.resolve()
    stubRects()
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observe() {}
        disconnect() {}
      }
    )
  })

  // Unmount even after a failed assertion so its window listeners cannot
  // answer the next test's resize.
  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  // The copy hides in immersive mode, so the hub has to move off the small
  // stage strip and centre on the whole stage, then come back on exit.
  it('relays the map out when immersive mode toggles', async () => {
    wrapper = mount(Stage, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(lastFocusHeight()).toBe(160)

    await wrapper.setProps({ immersive: true })
    await flushPromises()
    expect(lastFocusHeight()).toBe(700)

    await wrapper.setProps({ immersive: false })
    await flushPromises()
    expect(lastFocusHeight()).toBe(160)
  })

  // init() waits for the icons; a resize in that window used to be lost and
  // left the canvas buffer at the old size until the next resize.
  it('applies a viewport resize that lands while the scene is loading', async () => {
    let finishInit = () => {}
    initGate.wait = new Promise<void>((resolve) => (finishInit = resolve))
    wrapper = mount(Stage, { global: { plugins: [i18n] } })
    await flushPromises()
    resize.mockClear()

    window.dispatchEvent(new Event('resize'))
    await new Promise((resolve) => setTimeout(resolve, 250))

    expect(resize).toHaveBeenCalledTimes(1)
    finishInit()
    await flushPromises()
  })

  // A phone's address bar or a window resize can land while the page is
  // scrolled; the scroll must not count as navigation bar over the map, or the
  // markers near the top stay hidden after scrolling back.
  it('measures the navigation bar over the map from the top of the page', async () => {
    const navbar = document.body.appendChild(document.createElement('header'))
    navbar.className = 'app-navbar'
    let scrolled = 0
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(
      function (this: HTMLElement) {
        if (this === navbar)
          return DOMRect.fromRect({ x: 0, y: 0, width: 390, height: 64 })
        const entry = Object.entries(RECTS).find(([attr]) =>
          this.hasAttribute(attr)
        )
        const [top, height] = entry?.[1] ?? [0, 844]
        return DOMRect.fromRect({ x: 0, y: top - scrolled, width: 390, height })
      }
    )
    const topInset = () => resize.mock.lastCall?.[2]

    wrapper = mount(Stage, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(topInset()).toBe(64)

    scrolled = 400
    vi.stubGlobal('scrollY', scrolled)
    window.dispatchEvent(new Event('resize'))
    await new Promise((resolve) => setTimeout(resolve, 250))

    expect(topInset()).toBe(64)
    navbar.remove()
  })
})
