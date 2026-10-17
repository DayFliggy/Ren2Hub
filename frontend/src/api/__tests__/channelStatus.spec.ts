import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  channelOperation,
  parseChannelPage,
  parseChannelStatusDetails,
} from '../channelStatus'
import { httpTransport } from '../httpTransport'

const channel = {
  id: 1,
  name: 'Native',
  type: 1,
  group: 'default',
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
