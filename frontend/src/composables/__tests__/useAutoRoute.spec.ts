import { describe, expect, it } from 'vitest'
import { buildRouteGroupList } from '@/composables/useAutoRoute'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
describe('native channel display grouping', () => {
  it('groups only by supplied lab labels and counts backend eligibility', () => {
    const base = modelCatalogFixture().channels[0]!
    const groups = buildRouteGroupList([
      base,
      {
        ...base,
        id: 10,
        eligible: false,
        filter_reason: 'channel_saturated',
        in_flight: 3,
      },
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({
      groupName: 'Fixture lab',
      activeCount: 1,
    })
    expect(groups[0]!.channels.map((channel) => channel.id)).toEqual([9, 10])
    expect(groups[0]!.channels[1]).not.toHaveProperty('score')
    expect(groups[0]!.channels[1]).not.toHaveProperty('queue')
  })
})
