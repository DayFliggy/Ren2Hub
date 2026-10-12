import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import CountdownPill from '../CountdownPill.vue'

afterEach(() => vi.useRealTimers())

describe('activity countdown', () => {
  it('refreshes every subscriber when visible and releases the shared clock', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-12T12:00:00Z'))
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    const add = vi.spyOn(document, 'addEventListener')
    const remove = vi.spyOn(document, 'removeEventListener')
    const options = {
      props: { end: Date.now() / 1000 + 60 },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            messages: {
              en: {
                activity: {
                  countdown: {
                    ended: 'Ended',
                    untilEndShort: '{time}',
                  },
                },
              },
            },
          }),
        ],
      },
    }
    const first = mount(CountdownPill, options)
    const second = mount(CountdownPill, options)
    try {
      await nextTick()
      expect(first.text()).toBe('00:01:00')
      hidden.mockReturnValue(true)
      document.dispatchEvent(new Event('visibilitychange'))
      vi.setSystemTime(new Date('2026-10-12T12:02:00Z'))
      hidden.mockReturnValue(false)
      document.dispatchEvent(new Event('visibilitychange'))
      await nextTick()
      expect(first.text()).toBe('Ended')
      expect(second.text()).toBe('Ended')

      first.unmount()
      await second.setProps({ end: Date.now() / 1000 + 60 })
      vi.setSystemTime(new Date('2026-10-12T12:03:00Z'))
      document.dispatchEvent(new Event('visibilitychange'))
      await nextTick()
      expect(second.text()).toBe('Ended')
      expect(
        add.mock.calls.filter(([event]) => event === 'visibilitychange')
      ).toHaveLength(1)
    } finally {
      first.unmount()
      second.unmount()
    }
    expect(
      remove.mock.calls.filter(([event]) => event === 'visibilitychange')
    ).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})
