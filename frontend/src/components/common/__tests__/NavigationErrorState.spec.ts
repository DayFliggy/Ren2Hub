import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import NavigationErrorState from '@/components/common/NavigationErrorState.vue'
import i18n from '@/i18n'

const t = i18n.global.t
const nativeLocation = window.location

afterEach(() => {
  Object.defineProperty(window, 'location', {
    value: nativeLocation,
    configurable: true,
    writable: true,
  })
})

describe('NavigationErrorState', () => {
  it('explains a rate limit instead of a rendering error and links home', () => {
    const wrapper = mount(NavigationErrorState, {
      props: { failure: { kind: 'rate-limited', href: '/pricing' } },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('h1').text()).toBe(
      t('common.navigationError.rateLimitedTitle')
    )
    expect(wrapper.text()).toContain(
      t('common.navigationError.rateLimitedMessage')
    )
    expect(wrapper.text()).not.toContain(t('common.appError.message'))
    expect(wrapper.get('[data-navigation-home]').attributes('href')).toBe('/')
    expect(wrapper.find('[data-navigation-retry]').exists()).toBe(true)
  })

  it('offers the same home link for other navigation failures', () => {
    const wrapper = mount(NavigationErrorState, {
      props: { failure: { kind: 'failed', href: '/sign-in' } },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('h1').text()).toBe(t('common.navigationError.title'))
    expect(wrapper.text()).toContain(t('common.navigationError.message'))
    expect(wrapper.get('[data-navigation-home]').attributes('href')).toBe('/')
  })

  it('retries the failed target with its query and hash instead of the current page', async () => {
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      value: { ...nativeLocation, assign },
      configurable: true,
      writable: true,
    })
    const href = '/pricing?model=gpt%2F4&unit=K#prices'
    const wrapper = mount(NavigationErrorState, {
      props: { failure: { kind: 'rate-limited', href } },
      global: { plugins: [i18n] },
    })

    await wrapper.get('[data-navigation-retry]').trigger('click')

    expect(assign).toHaveBeenCalledExactlyOnceWith(href)
  })
})
