import { beforeAll, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TokenTrendCard from '../TokenTrendCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
vi.mock('@/charts/useEChart', () => ({ useEChart: vi.fn() }))
beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})
it('states the quota_data flush lag without inferring a cache hit rate', () => {
  const wrapper = mount(TokenTrendCard, {
    props: {
      points: [
        { date: '2026-10-18', total: 150, cache_create: 0, cache_read: 25 },
      ],
      flushIntervalMinutes: 5,
    },
    global: { plugins: [i18n] },
  })
  expect(wrapper.get('[data-cache-coverage]').attributes('title')).toContain(
    'written every 5 minutes, so the latest hour may be incomplete'
  )
  expect(wrapper.text()).not.toContain('Cross-protocol hit rate')
  expect(wrapper.text()).not.toContain('%')
  wrapper.unmount()
})

it('shows a skipped-window reason instead of a zero-value chart', async () => {
  const wrapper = mount(TokenTrendCard, {
    props: { points: [], unavailableReason: 'Log range exceeded' },
    global: { plugins: [i18n] },
  })

  expect(wrapper.get('[data-token-trend-unavailable]').text()).toContain(
    'Log range exceeded'
  )
  expect(wrapper.find('[role="img"]').exists()).toBe(false)
  await wrapper.setProps({ unavailableReason: null })
  expect(wrapper.find('[data-token-trend-unavailable]').exists()).toBe(false)
  expect(wrapper.find('[role="img"]').exists()).toBe(true)
  wrapper.unmount()
})
