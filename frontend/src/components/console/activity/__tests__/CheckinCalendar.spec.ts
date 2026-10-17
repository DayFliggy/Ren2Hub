import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { expect, it } from 'vitest'
import CheckinCalendar from '../CheckinCalendar.vue'
it('marks only server-returned check-ins and respects leap months without predicting claims', () => {
  const wrapper = mount(CheckinCalendar, {
    props: {
      month: '2024-02',
      records: [
        { checkin_date: '2024-02-29', quota_awarded: 500000 },
        { checkin_date: '2024-03-01', quota_awarded: 1 },
      ],
    },
    global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
  })
  expect(wrapper.findAll('[data-checkin-record]')).toHaveLength(1)
  expect(
    wrapper.get('[data-checkin-record]').attributes('data-checkin-record')
  ).toBe('2024-02-29')
  expect(
    wrapper.get('[data-checkin-record]').attributes('aria-label')
  ).toContain('$1.00')
  expect(wrapper.find('[aria-label="2024-02-30"]').exists()).toBe(false)
  expect(wrapper.find('button').exists()).toBe(false)
})
