import { mount } from '@vue/test-utils'
import { beforeAll, expect, it, vi } from 'vitest'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import InviteMonthChart from '../InviteMonthChart.vue'

vi.mock('@/charts/useEChart', () => ({ useEChart: vi.fn() }))

beforeAll(async () => {
  await loadMessageDomain('console')
})

it('labels the registration count chart with people rather than rewards', async () => {
  await setLocale('zh-CN')
  const wrapper = mount(InviteMonthChart, {
    props: {
      series: [
        { month: '2026-09', new_count: 2, cumulative: 5, reward: '0' },
        { month: '2026-10', new_count: 3, cumulative: 8, reward: '1000000' },
      ],
    },
    global: { plugins: [i18n] },
  })
  expect(wrapper.text()).toContain('每月邀请注册')
  // The total covers the months the server returned, not a fixed window.
  expect(wrapper.text()).toContain('5 人')
  expect(wrapper.text()).toContain('近 2 个月合计')
  expect(wrapper.text()).not.toContain('返佣趋势')
  wrapper.unmount()
  await setLocale('en')
})
