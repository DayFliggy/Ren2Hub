import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import CheckinCard from '../CheckinCard.vue'
import type { CheckinStatus } from '@/composables/useActivity'
import { formatQuota } from '@/utils/format'

const status: CheckinStatus = {
  enabled: true,
  min_quota: 100,
  max_quota: 500000,
  stats: {
    total_quota: 500000,
    total_checkins: 1,
    checkin_count: 1,
    checked_in_today: false,
    records: [{ checkin_date: '2026-10-09', quota_awarded: 500000 }],
  },
}

function render(locale: string, records = status.stats.records) {
  return mount(CheckinCard, {
    props: {
      status: { ...status, stats: { ...status.stats, records } },
      month: '2026-10',
      claiming: false,
      disabled: false,
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale,
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
}

describe('check-in records ledger', () => {
  it.each([
    ['en', 'Check-in records'],
    ['zh-CN', '签到记录'],
  ])(
    'names the table and identifies dates and award columns in %s',
    (locale, title) => {
      const wrapper = render(locale)
      const table = wrapper.get('table')
      expect(table.get('caption').text()).toContain(title)
      expect(table.get('caption').text()).toContain('2026-10')
      expect(table.findAll('thead th[scope="col"]')).toHaveLength(2)
      expect(
        table.get('tbody th[scope="row"] time').attributes('datetime')
      ).toBe('2026-10-09')
      expect(table.get('tbody td').text()).toBe(formatQuota(500000))
      wrapper.unmount()
    }
  )

  it('keeps month selection and real claim events while exposing an empty record state', async () => {
    const wrapper = render('en', [])
    expect(wrapper.get('details').text()).toContain(
      'No check-ins recorded for this month.'
    )
    expect(wrapper.find('table').exists()).toBe(false)
    await wrapper.get('input[type=month]').setValue('2026-09')
    expect(wrapper.emitted('update:month')).toEqual([['2026-09']])
    await wrapper.get('[data-checkin-action]').trigger('click')
    expect(wrapper.emitted('checkin')).toEqual([[]])
    wrapper.unmount()
  })
})
