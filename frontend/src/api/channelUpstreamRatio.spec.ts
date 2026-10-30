import { describe, expect, it } from 'vitest'
import { ApiError } from '@/api/types'
import { parseChannelUpstreamRatio } from './channelUpstreamRatio'

const base = {
  channel_id: 7,
  kind: 'newapi',
  source: 'newapi',
  mode: 'auto',
  status: 'ok',
  ratio: '1.5',
  effective_ratio: '1.5',
  group_name: 'vip',
  mapping: 'name_match',
  token_name: 'vip',
  stale: false,
  received_at: 1700000000,
  next_probe_at: 1700001800,
  last_error: '',
  version: 3,
}

describe('parseChannelUpstreamRatio', () => {
  it('accepts a complete NewAPI record', () => {
    const parsed = parseChannelUpstreamRatio({
      ...base,
      groups: [{ name: 'vip', description: 'VIP', ratio: 1.5 }],
    })
    expect(parsed.ratio).toBe('1.5')
    expect(parsed.groups).toEqual([
      { name: 'vip', description: 'VIP', ratio: 1.5 },
    ])
    expect(parsed.sub2api).toBeNull()
  })

  it('rejects a ratio that is not a plain decimal', () => {
    expect(() => parseChannelUpstreamRatio({ ...base, ratio: '1e3' })).toThrow(
      ApiError
    )
  })

  it('keeps a zero peak multiplier', () => {
    const parsed = parseChannelUpstreamRatio({
      ...base,
      kind: 'sub2api',
      source: 'sub2api',
      sub2api: {
        group_rate_multiplier: 1,
        resolved_rate_multiplier: 1,
        peak_rate_enabled: true,
        peak_start: '09:00',
        peak_end: '18:00',
        peak_rate_multiplier: 0,
        observed_at: 1700000000,
      },
    })
    expect(parsed.sub2api?.peak_rate_multiplier).toBe(0)
  })

  it('rejects an unknown status code', () => {
    expect(() =>
      parseChannelUpstreamRatio({ ...base, status: 'unknown' })
    ).toThrow(ApiError)
  })
})
