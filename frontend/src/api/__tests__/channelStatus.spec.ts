import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  channelOperation,
  parseChannelPage,
  parseChannelStatusDetails,
} from '../channelStatus'
import { httpTransport } from '../httpTransport'
import channelList from '../../../tests/fixtures/native/channel-list.get.json'
import multiKeyList from '../../../tests/fixtures/native/channel-multi-key-list.get.json'

const channel = {
  id: 1,
  name: 'Native',
  type: 1,
  channel_ratio: '1',
  max_concurrency: 0,
  in_flight: 0,
  lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
  status: 3,
  priority: null,
  weight: null,
  used_quota: 500000,
  balance: 2.5,
  response_time: 150,
  test_time: 42,
  base_url: null,
  models: 'fixture',
  model_mapping: null,
  other_info: '',
}
afterEach(() => vi.restoreAllMocks())
describe('native channel contract', () => {
  it('reads disabled reason from other_info and retains integer usage', () => {
    expect(parseChannelStatusDetails(channel)).toMatchObject({
      used_quota: 500000,
      priority: 0,
      base_url: '',
      status_reason: '',
      status_time: 0,
    })
    expect(
      parseChannelStatusDetails({
        ...channel,
        other_info: JSON.stringify({
          status_reason: '<b>provider error</b>',
          status_time: 123,
        }),
      })
    ).toMatchObject({
      status_reason: '<b>provider error</b>',
      status_time: 123,
    })
    expect(
      parseChannelPage({ items: [channel], total: 1, type_counts: { '1': 1 } })
        .items
    ).toHaveLength(1)
  })
  it.each([
    { status_reason: 1 },
    { status_time: '1' },
    { status_time: -1 },
    { status_time: 0.1 },
  ])('rejects invalid native metadata %j', (info) => {
    expect(() =>
      parseChannelStatusDetails({
        ...channel,
        other_info: JSON.stringify(info),
      })
    ).toThrow('Invalid API response')
  })
  it('rejects former decimal usage instead of silently changing its unit', () => {
    expect(() =>
      parseChannelStatusDetails({ ...channel, used_quota: '1.00' })
    ).toThrow('Invalid API response')
  })
  it('handles top-level native operation results and rejects business failures', async () => {
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValueOnce({ success: true, time: 0.125 } as never)
      .mockResolvedValueOnce({
        success: false,
        message: 'provider failed',
        error_code: 'bad_key',
      } as never)
    expect(await channelOperation('/api/channel/test/1')).toMatchObject({
      time: 0.125,
    })
    await expect(channelOperation('/api/channel/test/1')).rejects.toMatchObject(
      { message: 'provider failed', code: 'bad_key' }
    )
    expect(request).toHaveBeenCalledTimes(2)
  })
})
describe('native channel list fixture', () => {
  it('keeps the backend lab and adds no supplier or lab group fields', () => {
    const page = parseChannelPage(channelList.data)
    expect(page.total).toBe(4)
    for (const [index, item] of page.items.entries()) {
      expect(item.lab).toEqual(channelList.data.items[index]!.lab)
      expect(item).not.toHaveProperty('supplier')
      expect(item).not.toHaveProperty('lab_group_slug')
      expect(item).not.toHaveProperty('lab_group_name')
    }
  })
})

describe('native multi-key channel info', () => {
  const items = () => structuredClone(multiKeyList.data.items)
  const byName = (name: string) =>
    parseChannelPage(multiKeyList.data).items.find(
      (item) => item.name === name
    )!

  it('parses the disabled-key list the backend returns', () => {
    expect(byName('contract-single').channel_info).toEqual({
      is_multi_key: false,
      multi_key_mode: '',
      multi_key_size: 0,
      multi_key_status_list: null,
    })
    expect(
      byName('contract-multi-new').channel_info?.multi_key_status_list
    ).toBeNull()
    expect(
      byName('contract-multi-restored').channel_info?.multi_key_status_list
    ).toEqual({})
    expect(byName('contract-multi-partial').channel_info).toEqual({
      is_multi_key: true,
      multi_key_mode: 'random',
      multi_key_size: 3,
      multi_key_status_list: { 1: 2, 2: 3 },
    })
  })

  it('rejects key states the backend never stores in the list', () => {
    for (const statusList of [{ 0: 1 }, { 0: 4 }, { 0: '2' }, { a: 2 }]) {
      const [item] = items()
      item!.channel_info.multi_key_status_list = statusList as never
      expect(() => parseChannelStatusDetails(item)).toThrow(
        'Invalid API response'
      )
    }
  })

  it('rejects a channel_info without its native fields', () => {
    const [item] = items()
    const { multi_key_size: _size, ...withoutSize } = item!.channel_info
    expect(() =>
      parseChannelStatusDetails({ ...item, channel_info: withoutSize })
    ).toThrow('Invalid API response')
    expect(() =>
      parseChannelStatusDetails({
        ...item,
        channel_info: { ...item!.channel_info, multi_key_size: -1 },
      })
    ).toThrow('Invalid API response')
  })
})
