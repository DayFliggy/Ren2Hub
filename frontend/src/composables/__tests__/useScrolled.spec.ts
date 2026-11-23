import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'

import { useScrolled } from '@/composables/useScrolled'

let frames: Map<number, FrameRequestCallback>
let nextFrame = 0

function setScrollY(value: number) {
  Object.defineProperty(window, 'scrollY', { value, configurable: true })
}

function flushFrames() {
  const pending = [...frames.values()]
  frames.clear()
  for (const callback of pending) callback(performance.now())
}

const Probe = defineComponent({
  setup() {
    const { scrolled } = useScrolled(20)
    return () => h('span', String(scrolled.value))
  },
})

beforeEach(() => {
  frames = new Map()
  vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
    nextFrame += 1
    frames.set(nextFrame, callback)
    return nextFrame
  })
  vi.spyOn(window, 'cancelAnimationFrame').mockImplementation((id) => {
    frames.delete(id)
  })
  setScrollY(0)
})

afterEach(() => {
  vi.restoreAllMocks()
  setScrollY(0)
})

describe('useScrolled', () => {
  it('reads the initial position on mount without waiting for a frame', async () => {
    setScrollY(40)
    const wrapper = mount(Probe)
    await wrapper.vm.$nextTick()

    expect(frames.size).toBe(0)
    expect(wrapper.text()).toBe('true')
    wrapper.unmount()
  })

  it('coalesces a burst of scroll events into one frame', async () => {
    const wrapper = mount(Probe)

    setScrollY(10)
    window.dispatchEvent(new Event('scroll'))
    setScrollY(60)
    window.dispatchEvent(new Event('scroll'))
    window.dispatchEvent(new Event('scroll'))

    expect(window.requestAnimationFrame).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toBe('false')

    flushFrames()
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toBe('true')
    wrapper.unmount()
  })

  it('cancels the pending frame on unmount', () => {
    const wrapper = mount(Probe)
    frames.clear()
    window.dispatchEvent(new Event('scroll'))
    const [pending] = [...frames.keys()]
    expect(frames.size).toBe(1)

    wrapper.unmount()

    expect(window.cancelAnimationFrame).toHaveBeenCalledWith(pending)
    expect(frames.size).toBe(0)
  })
})
