import { mount } from '@vue/test-utils'
import { expect, it } from 'vitest'
import i18n from '@/i18n'
import RuntimePulseBand from '../RuntimePulseBand.vue'
it('renders runtime and only reported availability', () => {
  const wrapper = mount(RuntimePulseBand, {
    props: {
      runtime: { days: 1, hours: 2, minutes: 3, seconds: 4 },
      uptimeLabel: null,
    },
    global: { plugins: [i18n] },
  })
  expect(wrapper.get('[role="timer"]').text()).toContain('001')
  expect(wrapper.find('.runtime-availability').exists()).toBe(false)
  expect(wrapper.get('[data-home-request-total]').text()).toBe('--')
  expect(wrapper.find('.runtime-trend svg').exists()).toBe(false)
  wrapper.unmount()
})
