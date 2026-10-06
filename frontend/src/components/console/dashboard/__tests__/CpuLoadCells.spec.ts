import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'

import CpuLoadCells, {
  type CpuLoadCell,
} from '@/components/console/dashboard/CpuLoadCells.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

function cell(minutes: number, overrides: Partial<CpuLoadCell> = {}) {
  return {
    minutes,
    value: '0.38',
    fill: 0.095,
    color: 'var(--glow)',
    overloaded: false,
    ...overrides,
  }
}

function mountCells(cells: CpuLoadCell[], cores: number | null = 4) {
  return mount(CpuLoadCells, {
    props: { cells, cores },
    global: { plugins: [i18n] },
  })
}

describe('CpuLoadCells', () => {
  it('lays out 1 / 5 / 15 minute cells with no visible label text', () => {
    const wrapper = mountCells([cell(1), cell(5), cell(15)])

    const cells = wrapper.findAll('[data-minutes]')
    expect(cells.map((item) => item.attributes('data-minutes'))).toEqual([
      '1',
      '5',
      '15',
    ])
    // Figures exist only for the stretched state and stay out of the a11y tree.
    expect(wrapper.find('.cpu-load__value').attributes('aria-hidden')).toBe(
      'true'
    )
    expect(cells[0]!.attributes('tabindex')).toBe('0')
    expect(cells[0]!.attributes('aria-label')).toBe(
      '1-minute load average 0.38 across 4 cores'
    )
  })

  it('names the window when the core count or the reading is missing', () => {
    const noCores = mountCells([cell(5, { fill: null })], null)
    expect(noCores.find('[data-minutes]').attributes('aria-label')).toBe(
      '5-minute load average 0.38'
    )
    expect(noCores.find('[data-cpu-load-fill]').exists()).toBe(false)

    const unknown = mountCells([cell(15, { value: null, fill: null })])
    const item = unknown.find('[data-minutes]')
    expect(item.attributes('data-unknown')).toBe('')
    expect(item.attributes('aria-label')).toBe(
      '15-minute load average unavailable'
    )
    expect(item.text()).toBe('--')
  })

  it('caps an overloaded cell at full height and keeps idle load visible', () => {
    const wrapper = mountCells([
      cell(1, { value: '6.40', fill: 1.6, overloaded: true }),
      cell(5, { value: '0.01', fill: 0.0025 }),
      cell(15, { value: '0.00', fill: 0 }),
    ])

    const fills = wrapper.findAll('[data-cpu-load-fill]')
    expect(fills.map((fill) => fill.attributes('style'))).toEqual([
      'height: 100%;',
      'height: 8%;',
      'height: 0%;',
    ])
    expect(wrapper.find('.cpu-load__value--over').text()).toBe('6.40')
  })
})
