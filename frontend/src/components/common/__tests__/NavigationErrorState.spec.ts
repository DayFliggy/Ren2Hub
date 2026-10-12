import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import NavigationErrorState from '@/components/common/NavigationErrorState.vue'
import i18n from '@/i18n'

const t = i18n.global.t

describe('NavigationErrorState', () => {
  it('explains a rate limit instead of a rendering error and links home', () => {
    const wrapper = mount(NavigationErrorState, {
      props: { failure: 'rate-limited' },
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
      props: { failure: 'failed' },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('h1').text()).toBe(t('common.navigationError.title'))
    expect(wrapper.text()).toContain(t('common.navigationError.message'))
    expect(wrapper.get('[data-navigation-home]').attributes('href')).toBe('/')
  })
})
