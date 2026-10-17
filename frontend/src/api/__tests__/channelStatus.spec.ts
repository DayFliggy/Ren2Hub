import { describe, expect, it } from 'vitest'
import { parseChannelStatusDetails } from '../channelStatus'
import type { AdminChannel } from '@/types/console'
describe('channel status details', () => {
  it('preserves server text and time with empty defaults for old responses', () => {
    expect(parseChannelStatusDetails({ id: 1 } as AdminChannel)).toMatchObject({
      status_reason: '',
      status_time: 0,
    })
    expect(
      parseChannelStatusDetails({
        status_reason: '<b>provider error</b>',
        status_time: 123,
      } as AdminChannel)
    ).toMatchObject({
      status_reason: '<b>provider error</b>',
      status_time: 123,
    })
  })
  it.each([
    { status_reason: 1 },
    { status_time: '1' },
    { status_time: -1 },
    { status_time: 0.1 },
  ])('rejects an invalid existing field %j', (value) => {
    expect(() =>
      parseChannelStatusDetails(value as unknown as AdminChannel)
    ).toThrow('Invalid API response')
  })
})
