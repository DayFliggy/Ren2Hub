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
        { month: '2026-10', new_count: 3, cumulative: 8, reward: '1000000' },
      ],
    },
    global: { plugins: [i18n] },
  })
  expect(wrapper.text()).toContain('近 6 个月邀请注册')
  expect(wrapper.text()).toContain('每月通过你的邀请码完成注册的用户')
  expect(wrapper.text()).toContain('3 人')
  expect(wrapper.text()).not.toContain('返佣趋势')
  wrapper.unmount()
  await setLocale('en')
})
