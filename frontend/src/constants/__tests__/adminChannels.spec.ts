import { describe, expect, it } from 'vitest'

import { parseChannelPage } from '@/api/channelStatus'
import {
  groupChannelsByLab,
  hasInvalidModelMapping,
} from '@/constants/adminChannels'
import channelList from '../../../tests/fixtures/native/channel-list.get.json'

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
