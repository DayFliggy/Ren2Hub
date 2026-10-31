import { describe, expect, it } from 'vitest'

import { parseSystemStatus, type SystemStatus } from '@/api/systemStatus'
import { systemStatusFixture } from '@/components/console/dashboard/__tests__/systemStatusFixture'

/** A JSON round trip, as the transport delivers it; invalid edits cast. */
function payload(): SystemStatus {
  return JSON.parse(JSON.stringify(systemStatusFixture())) as SystemStatus
}

describe('parseSystemStatus', () => {
  it('accepts the shared snapshot contract unchanged', () => {
    expect(parseSystemStatus(payload())).toEqual(systemStatusFixture())
  })
  it('keeps a missing load sample and unknown core count as null', () => {
    const raw = payload()
    raw.load = null
    expect(parseSystemStatus(raw).load).toBeNull()
    const withoutCores = payload()
    withoutCores.load!.cores = null
    expect(parseSystemStatus(withoutCores).load?.cores).toBeNull()
  })
  it.each([
    ['a rate without the ok state', { state: 'no_requests', rate: 50 }],
    ['the ok state without a rate', { state: 'ok', rate: null }],
    ['a rate above 100', { state: 'ok', rate: 101 }],
    ['an unknown state', { state: 'partial', rate: null }],
  ])('rejects %s', (_name, change) => {
    const raw = payload()
    raw.success_rate = { ...raw.success_rate, ...change } as never
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
  })
  it('rejects readings on an unavailable resource and zero cores', () => {
    const raw = payload()
    raw.memory.available = false
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
    const zero = payload()
    zero.load!.cores = 0
    expect(() => parseSystemStatus(zero)).toThrow('Invalid API response')
  })
  it('keeps the reading of a resource whose threshold check is off', () => {
    const raw = payload()
    raw.disk.state = 'unchecked'
    expect(parseSystemStatus(raw).disk).toMatchObject({
      state: 'unchecked',
      percent: 42.6,
    })
    raw.disk.state = 'partial' as never
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
  })
  it.each([undefined, 'warning', 'database'])(
    'rejects a storage verdict of %s',
    (storage) => {
      const raw = payload()
      Object.assign(raw, { storage_status: storage })
      expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
    }
  )
  it('rejects Root-only diagnostics in place of the projection', () => {
    const { cpu, ...raw } = payload()
    Object.assign(raw, { resources: { cpu } })
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
  })
})
