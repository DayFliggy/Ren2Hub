import { beforeAll, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import TokenTrendCard from '../TokenTrendCard.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
vi.mock('@/charts/useEChart', () => ({ useEChart: vi.fn() }))
beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})
it('shows known cache coverage without inferring a cross-protocol hit rate', () => {
  const wrapper = mount(TokenTrendCard, {
    props: {
      points: [
        {
          date: '2026-10-18',
          total: 150,
          input: 100,
          output: 50,
          cache_create: null,
          cache_read: 25,
          hit_rate: null,
        },
      ],
      cacheCoverage: {
        records: 3,
        read_records: 1,
        write_records: 0,
        invalid_records: 1,
      },
    },
    global: { plugins: [i18n] },
  })
  expect(wrapper.get('[data-cache-coverage]').text()).toContain(
    '1 read, 0 write and 1 invalid records out of 3'
  )
  expect(wrapper.text()).toContain('Cross-protocol hit rate is unknown')
  expect(wrapper.text()).not.toContain('20%')
  expect(wrapper.text()).not.toContain('0%')
  wrapper.unmount()
})
