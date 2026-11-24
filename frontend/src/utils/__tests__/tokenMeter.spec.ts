import { describe, expect, it } from 'vitest'

import { tokenMeter, tokenMeterLabel, tokenMeterNote } from '@/utils/tokenMeter'

const DAY = 86_400
// Local noon keeps the formatted calendar day independent of the runner's zone.
const now = new Date(2026, 10, 17, 12).getTime() / 1000
// Blocked rings are a faded neutral, never the healthy --signal colour.
const blocked = 'color-mix(in srgb, var(--text-tertiary) 45%, transparent)'

const key = (patch: Partial<Parameters<typeof tokenMeter>[0]> = {}) => ({
  status: 1,
  unlimited: false,
  remain_quota: 750,
  used_quota: '250',
  expired_time: -1,
  ...patch,
})

function meterOf(patch: Partial<Parameters<typeof tokenMeter>[0]> = {}) {
  const token = key(patch)
  const meter = tokenMeter(token, now)
  return { meter, note: tokenMeterNote(token, meter) }
}

describe('tokenMeter', () => {
  it('draws the consumed share and stays quiet while the key is healthy', () => {
    const { meter, note } = meterOf()
    expect(meter).toEqual({
      state: 'normal',
      arcPercent: 25,
      usedPercent: 25,
      remainingPercent: 75,
      expiresInDays: null,
      ringColor: 'var(--signal)',
      remainingColor: 'var(--text-primary)',
    })
    expect(note).toEqual([{ key: 'common.never' }])
  })

  it('keeps a visible sliver for any spend and an empty ring for none', () => {
    expect(meterOf({ remain_quota: 999, used_quota: '1' }).meter).toMatchObject(
      {
        arcPercent: 5,
        usedPercent: 0,
      }
    )
    expect(
      meterOf({ remain_quota: 1000, used_quota: '0' }).meter.arcPercent
    ).toBe(0)
  })

  it('warns strictly below a tenth remaining, with the share in the note', () => {
    expect(meterOf({ remain_quota: 100, used_quota: '900' }).meter.state).toBe(
      'normal'
    )
    const { meter, note } = meterOf({ remain_quota: 99, used_quota: '901' })
    expect(meter).toMatchObject({
      state: 'low',
      ringColor: 'var(--status-warning)',
    })
    // Only the scarcity is coloured, not the expiry beside it.
    expect(note).toEqual([
      { key: 'keys.meter.onlyLeft', params: { percent: 10 }, tone: 'warning' },
      { key: 'common.never' },
    ])
  })

  it('never reports 0% left or 100% used while a low balance is still active', () => {
    const { meter, note } = meterOf({ remain_quota: 500, used_quota: '999500' })
    expect(meter).toMatchObject({
      state: 'low',
      usedPercent: 99,
      remainingPercent: 1,
    })
    expect(note).toEqual([
      { key: 'keys.meter.onlyLeft', params: { percent: 1 }, tone: 'warning' },
      { key: 'common.never' },
    ])
  })

  it('fills the ring red when the balance is spent or was never funded', () => {
    for (const patch of [
      { remain_quota: 0, used_quota: '500' },
      { remain_quota: 0, used_quota: '0' },
      { status: 4, remain_quota: 0, used_quota: '500' },
    ]) {
      const { meter, note } = meterOf(patch)
      expect(meter).toMatchObject({
        state: 'exhausted',
        arcPercent: 100,
        ringColor: 'var(--status-danger)',
        remainingColor: 'var(--status-danger-text)',
      })
      expect(note).toEqual([{ key: 'keys.statusExhausted', tone: 'danger' }])
    }
  })

  it('keeps a switched-off key neutral even when its balance is spent', () => {
    const { meter, note } = meterOf({ status: 2, remain_quota: 0 })
    expect(meter).toMatchObject({
      state: 'disabled',
      arcPercent: 100,
      ringColor: blocked,
      remainingColor: 'var(--text-secondary)',
      expiresInDays: null,
    })
    expect(note).toEqual([{ key: 'common.disabled' }])
    expect(meterOf({ status: 9 }).note).toEqual([{ key: 'common.unknown' }])
  })

  it('treats a passed expiry as expired before the backend flips the status', () => {
    const expiredAt = new Date(2026, 10, 14, 12).getTime() / 1000
    for (const patch of [
      { status: 3, expired_time: expiredAt },
      { status: 1, expired_time: expiredAt },
    ]) {
      const { meter, note } = meterOf(patch)
      expect(meter).toMatchObject({
        state: 'expired',
        arcPercent: 100,
        ringColor: blocked,
        remainingColor: 'var(--text-tertiary)',
      })
      expect(note).toEqual([
        { key: 'keys.meter.expiredOn', params: { date: '2026-11-14' } },
      ])
    }
    // The backend only expires a key strictly after its timestamp.
    expect(meterOf({ expired_time: now }).meter).toMatchObject({
      state: 'normal',
      expiresInDays: 1,
    })
  })

  it('warns within a week of expiry and shows the date beyond it', () => {
    const soon = meterOf({ expired_time: now + 2 * DAY - 3600 })
    expect(soon.meter).toMatchObject({
      state: 'normal',
      expiresInDays: 2,
      ringColor: 'var(--status-warning)',
    })
    expect(soon.note).toEqual([
      { key: 'keys.meter.expiresInDays', params: { days: 2 }, tone: 'warning' },
    ])
    expect(meterOf({ expired_time: now + 7 * DAY }).meter.expiresInDays).toBe(7)

    const later = meterOf({ expired_time: now + 7 * DAY + 1 })
    expect(later.meter).toMatchObject({
      expiresInDays: null,
      ringColor: 'var(--signal)',
    })
    expect(later.note).toEqual([
      { key: 'keys.meter.expiresOn', params: { date: '2026-11-24' } },
    ])
  })

  it('never reports an unlimited key as exhausted', () => {
    const { meter } = meterOf({
      unlimited: true,
      remain_quota: 0,
      used_quota: '6000000',
    })
    expect(meter).toMatchObject({
      state: 'unlimited',
      arcPercent: 100,
      usedPercent: null,
      remainingPercent: null,
      ringColor: 'var(--signal)',
    })
    expect(
      meterOf({ unlimited: true, remain_quota: 0, expired_time: now + DAY })
        .meter.ringColor
    ).toBe('var(--status-warning)')
  })

  it('labels the popover status by what blocks the key', () => {
    const label = (patch: Partial<Parameters<typeof tokenMeter>[0]>) => {
      const token = key(patch)
      return tokenMeterLabel(token, tokenMeter(token, now))
    }
    expect(label({})).toBe('common.enabled')
    expect(label({ remain_quota: 99, used_quota: '901' })).toBe(
      'common.enabled'
    )
    expect(label({ status: 2 })).toBe('common.disabled')
    expect(label({ expired_time: now - 1 })).toBe('keys.statusExpired')
    expect(label({ remain_quota: 0 })).toBe('keys.statusExhausted')
  })
})
