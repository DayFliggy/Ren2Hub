import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import MiniSparkline from '@/components/console/dashboard/MiniSparkline.vue'

describe('MiniSparkline', () => {
  it('keeps smoothed control points inside the projected value range', () => {
    const wrapper = mount(MiniSparkline, {
      props: { points: [0, 100, 100, 100], height: 32 },
    })
    const path = wrapper.find('path[fill="none"]').attributes('d') ?? ''
    const coordinates = path.match(/-?\d+(?:\.\d+)?/g)?.map(Number) ?? []
    const yCoordinates = coordinates.filter((_, index) => index % 2 === 1)

    expect(path).toContain('C')
    expect(Math.min(...yCoordinates)).toBeGreaterThanOrEqual(2)
    expect(Math.max(...yCoordinates)).toBeLessThanOrEqual(30)
  })
})

describe('MiniSparkline gaps', () => {
  it('draws the same single path when no point is missing', () => {
    const wrapper = mount(MiniSparkline, { props: { points: [1, 3, 2] } })
    const line = wrapper.find('path[fill="none"]').attributes('d') ?? ''
    expect(line.match(/M/g)).toHaveLength(1)
    expect(wrapper.findAll('circle')).toHaveLength(1)
  })
  it('breaks the line at a gap and drops a lone measured point', () => {
    const wrapper = mount(MiniSparkline, {
      props: { points: [1, 2, null, 4, null, 3, 5] },
    })
    const line = wrapper.find('path[fill="none"]').attributes('d') ?? ''
    expect(line.match(/M/g)).toHaveLength(2)
    const area = wrapper.find('path[fill^="url"]').attributes('d') ?? ''
    expect(area.match(/Z/g)).toHaveLength(2)
  })
  it('marks no latest point when the newest sample is missing', () => {
    const wrapper = mount(MiniSparkline, { props: { points: [1, 2, null] } })
    expect(wrapper.find('path[fill="none"]').exists()).toBe(true)
    expect(wrapper.findAll('circle')).toHaveLength(0)
  })
  it('renders nothing when no two adjacent points were measured', () => {
    const wrapper = mount(MiniSparkline, {
      props: { points: [1, null, 2, null] },
    })
    expect(wrapper.find('svg').exists()).toBe(false)
  })
})
