import { describe, expect, it } from 'vitest'

import { parseChannelPage } from '@/api/channelStatus'
import {
  groupChannelsByLab,
  hasInvalidModelMapping,
  multiKeyAvailability,
} from '@/constants/adminChannels'
import channelList from '../../../tests/fixtures/native/channel-list.get.json'
import multiKeyList from '../../../tests/fixtures/native/channel-multi-key-list.get.json'

// The fixture is written by TestFrontendContractChannelList from the real
// /api/channel/ handler, so the labs below are the backend classification.
const channels = parseChannelPage(channelList.data).items

describe('backend channel lab grouping', () => {
  it('groups by lab id and names the unknown and mixed classifications', () => {
    const groups = groupChannelsByLab(channels).map((group) => ({
      groupSlug: group.groupSlug,
      supplier: group.supplier,
      ids: group.channels.map((channel) => channel.id),
    }))
    expect(groups).toEqual([
      { groupSlug: 'unknown', supplier: 'channels.labUnknown', ids: [4, 3] },
      { groupSlug: 'mixed', supplier: 'channels.labMixed', ids: [2] },
      { groupSlug: 'openai', supplier: 'OpenAI', ids: [1] },
    ])
  })

  it('flags only the channel whose model mapping the backend rejected', () => {
    const flagged = channels.filter((channel) =>
      hasInvalidModelMapping(channel.lab)
    )
    expect(flagged.map((channel) => channel.name)).toEqual(['contract-invalid'])
  })
})

// Written by TestFrontendContractMultiKeyChannelList from the real
// /api/channel/ handler, seeded with the key states the native paths store.
const multiKeyChannels = parseChannelPage(multiKeyList.data).items

describe('multi-key availability', () => {
  const availability = (name: string) =>
    multiKeyAvailability(
      multiKeyChannels.find((channel) => channel.name === name)!
    )

  it('counts every key as enabled when no key is disabled', () => {
    for (const name of ['contract-multi-new', 'contract-multi-restored']) {
      expect(availability(name)).toEqual({ enabled: 2, total: 2, disabled: [] })
    }
  })

  it('lists disabled keys by 1-based position with their native state', () => {
    expect(availability('contract-multi-partial')).toEqual({
      enabled: 1,
      total: 3,
      disabled: [
        { position: 2, status: 2 },
        { position: 3, status: 3 },
      ],
    })
    expect(availability('contract-multi-exhausted')).toMatchObject({
      enabled: 0,
      total: 2,
    })
  })

  it('has nothing to show for single-key channels', () => {
    expect(availability('contract-single')).toBeNull()
  })
})
