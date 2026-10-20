import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'

import StatsModelTable from '@/components/console/dashboard/stats/StatsModelTable.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { StatsModelRow } from '@/composables/useDashboardStats'
import {
  captureResizeObservers,
  resizeObservers,
  setScrollGeometry,
} from './scrollFadeTestUtils'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

beforeEach(() => {
  captureResizeObservers()
})

const sampleModels: StatsModelRow[] = [
  {
    model: 'gpt-4o',
    tokens: 120_000,
    quota: '600000',
    requests: 45,
    share: 60,
    avgLatency: 120,
  },
  {
    model: 'claude-3-5-sonnet',
    tokens: 80_000,
    quota: '400000',
    requests: 30,
    share: 40,
    avgLatency: 150,
  },
]

function render(models: StatsModelRow[] = sampleModels, loading = false) {
  return mount(StatsModelTable, {
    props: { models, loading },
    global: { plugins: [i18n] },
  })
}

describe('StatsModelTable', () => {
  it('renders model breakdown rows and headers', () => {
    const wrapper = render()

    expect(wrapper.find('[data-stats-model-scroll]').exists()).toBe(true)
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).toContain('gpt-4o')
    expect(wrapper.text()).toContain('claude-3-5-sonnet')
  })

  it('renders empty state when models list is empty', () => {
    const wrapper = render([])

    expect(wrapper.text()).toContain('No data')
  })

  it('shows edge fade on horizontal overflow and hides it when scrolled to end or not overflowing', async () => {
    const wrapper = render()
    const scroll = wrapper.find('[data-stats-model-scroll]')
    const el = scroll.element as HTMLElement

    // Default without overflow
    expect(wrapper.find('[data-table-scroll-fade]').exists()).toBe(false)

    setScrollGeometry(el, { clientWidth: 300, scrollWidth: 600 })
    await scroll.trigger('scroll')
    expect(wrapper.find('[data-table-scroll-fade]').exists()).toBe(true)

    // Scrolled to end
    el.scrollLeft = 300
    await scroll.trigger('scroll')
    expect(wrapper.find('[data-table-scroll-fade]').exists()).toBe(false)

    // No overflow
    setScrollGeometry(el, { clientWidth: 300, scrollWidth: 300 })
    await scroll.trigger('scroll')
    expect(wrapper.find('[data-table-scroll-fade]').exists()).toBe(false)
  })

  it('rebinds scroll and resize tracking to the table that replaces it on a range change', async () => {
    const wrapper = render()
    const first = wrapper.find('[data-stats-model-scroll]').element

    // A range change shows the skeleton, then renders a brand-new table.
    await wrapper.setProps({ loading: true })
    await wrapper.setProps({ loading: false, models: [...sampleModels] })
    await flushPromises()

    const scroll = wrapper.find('[data-stats-model-scroll]')
    const el = scroll.element as HTMLElement
    expect(el).not.toBe(first)
    expect(resizeObservers.some((o) => o.targets.has(first))).toBe(false)
    expect(resizeObservers.some((o) => o.targets.has(el))).toBe(true)

    // The listener lives on the new element, not on the detached one.
    setScrollGeometry(el, { clientWidth: 300, scrollWidth: 600 })
    el.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(wrapper.find('[data-table-scroll-fade]').exists()).toBe(true)

    el.scrollLeft = 300
    el.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(wrapper.find('[data-table-scroll-fade]').exists()).toBe(false)
  })
})
