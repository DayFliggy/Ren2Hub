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
  it('renders three visual cells (1/5/15 min) inside a single button', () => {
    const wrapper = mountCells([cell(1), cell(5), cell(15)])

    const trigger = wrapper.find('.cpu-load__trigger')
    expect(trigger.attributes('type')).toBe('button')
    expect(trigger.attributes('aria-label')).toContain('Host load average')
    expect(trigger.attributes('aria-label')).toContain('1 min 0.38')
    expect(trigger.attributes('aria-label')).toContain('5 min 0.38')
    expect(trigger.attributes('aria-label')).toContain('15 min 0.38')
    expect(trigger.attributes('aria-label')).toContain('4 cores')

    const cells = wrapper.findAll('[data-minutes]')
    expect(cells.map((item) => item.attributes('data-minutes'))).toEqual([
      '1',
      '5',
      '15',
    ])
    expect(wrapper.find('[data-cpu-load-fill]').exists()).toBe(true)
  })

  it('toggles the readout popover on click', async () => {
    const wrapper = mountCells([
      cell(1, { value: '0.38' }),
      cell(5, { value: '0.20' }),
      cell(15, { value: '0.18' }),
    ])

    const trigger = wrapper.find('.cpu-load__trigger')
    const readout = wrapper.find('[data-load-readout]')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(readout.classes()).not.toContain('cpu-load__readout--open')

    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(readout.classes()).toContain('cpu-load__readout--open')

    // Readout contains three windows
    const windows = readout.findAll('[data-load-window]')
    expect(windows).toHaveLength(3)
    expect(windows[0]!.attributes('data-load-window')).toBe('1')
    expect(windows[1]!.attributes('data-load-window')).toBe('5')
    expect(windows[2]!.attributes('data-load-window')).toBe('15')
    expect(windows[0]!.text()).toContain('0.38')

    await trigger.trigger('click')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(readout.classes()).not.toContain('cpu-load__readout--open')
  })

  // WCAG 1.4.13: hover content stays while the pointer is over it and can be
  // dismissed with Escape without moving the pointer.
  it('keeps the hover readout open over the popover and dismisses on Escape', async () => {
    const wrapper = mountCells([cell(1), cell(5), cell(15)])
    const root = wrapper.find('[data-cpu-load]')
    const trigger = wrapper.find('.cpu-load__trigger')

    await root.trigger('pointerenter', { pointerType: 'mouse' })
    expect(trigger.attributes('aria-expanded')).toBe('true')

    // Clicking while hovering does not toggle the readout shut.
    await trigger.trigger('click', { pointerType: 'mouse' })
    expect(trigger.attributes('aria-expanded')).toBe('true')

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(trigger.attributes('aria-expanded')).toBe('false')

    await root.trigger('pointerenter', { pointerType: 'mouse' })
    await root.trigger('pointerleave', { pointerType: 'mouse' })
    expect(trigger.attributes('aria-expanded')).toBe('false')
  })

  it('leaves touch taps to the click toggle', async () => {
    const wrapper = mountCells([cell(1), cell(5), cell(15)])
    const root = wrapper.find('[data-cpu-load]')
    const trigger = wrapper.find('.cpu-load__trigger')

    await root.trigger('pointerenter', { pointerType: 'touch' })
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click', { pointerType: 'touch' })
    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  it('shows core count in readout header when available', () => {
    const wrapper = mountCells([cell(1), cell(5), cell(15)], 4)
    const header = wrapper.find('.cpu-load__header')
    expect(header.text()).toContain('Host load average')
    expect(header.text()).toContain('4 cores')
  })

  it('names the host scope without a core count when cores are unknown', () => {
    const wrapper = mountCells([cell(1), cell(5), cell(15)], null)
    expect(wrapper.get('.cpu-load__header').text()).toBe('Host load average')
    expect(
      wrapper.find('.cpu-load__trigger').attributes('aria-label')
    ).toContain('Host load average')
    expect(
      wrapper.find('.cpu-load__trigger').attributes('aria-label')
    ).not.toContain('cores')
  })

  it('marks overloaded values with danger color', async () => {
    const wrapper = mountCells([
      cell(1, { value: '6.40', fill: 1.6, overloaded: true }),
      cell(5, { value: '0.20', fill: 0.05 }),
      cell(15, { value: '0.18', fill: 0.045 }),
    ])

    await wrapper.find('.cpu-load__trigger').trigger('click')
    const figures = wrapper.findAll('.cpu-load__figure')
    expect(figures[0]!.classes()).toContain('cpu-load__figure--over')
    expect(figures[1]!.classes()).not.toContain('cpu-load__figure--over')
  })

  it('shows -- placeholder and sr-only text for unavailable windows', async () => {
    const wrapper = mountCells([
      cell(1, { value: null, fill: null }),
      cell(5, { value: '0.20', fill: 0.05 }),
      cell(15, { value: '0.18', fill: 0.045 }),
    ])

    const cells = wrapper.findAll('[data-minutes]')
    expect(cells[0]!.attributes('data-unknown')).toBe('')

    await wrapper.find('.cpu-load__trigger').trigger('click')
    const windows = wrapper.findAll('[data-load-window]')
    const unavailable = windows[0]!
    expect(unavailable.text()).toContain('--')
    expect(unavailable.find('.sr-only').text()).toBe('unavailable')
  })

  it('caps overloaded fill at 100% and keeps idle load slightly visible', () => {
    const wrapper = mountCells([
      cell(1, { value: '6.40', fill: 1.6, overloaded: true }),
      cell(5, { value: '0.01', fill: 0.0025 }),
      cell(15, { value: '0.00', fill: 0 }),
    ])

    const cells = wrapper.findAll('[data-minutes]')
    const styles = cells.map((cell) => cell.attributes('style'))

    expect(styles[0]).toContain('--fill-height: 100%')
    expect(styles[1]).toContain('--fill-height: 8%')
    expect(styles[2]).toContain('--fill-height: 0%')
  })
})
