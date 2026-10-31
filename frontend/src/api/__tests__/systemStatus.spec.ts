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
  it.each(['host_interface', 'application_listener'] as const)(
    'accepts %s traffic with its inbound and outbound rates',
    (scope) => {
      const raw = payload()
      raw.traffic.scope = scope
      raw.traffic.inbound_mbps = null
      expect(parseSystemStatus(raw).traffic).toMatchObject({
        scope,
        inbound_mbps: null,
        outbound_mbps: 12.4,
      })
    }
  )
  it.each([
    ['the retired body scope', { scope: 'application_http_body' }],
    ['an unknown scope', { scope: 'host_network' }],
    ['a negative rate', { outbound_mbps: -0.1 }],
    ['a string rate', { inbound_mbps: '2.1' }],
    ['a missing rate', { inbound_mbps: undefined }],
  ])('rejects traffic with %s', (_name, change) => {
    const raw = payload()
    Object.assign(raw.traffic, change)
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
  })
  it('rejects traffic under the retired field names', () => {
    const raw = payload()
    const { inbound_mbps, outbound_mbps, ...rest } = raw.traffic
    raw.traffic = {
      ...rest,
      request_mbps: inbound_mbps,
      response_mbps: outbound_mbps,
    } as never
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
    const point = payload()
    point.traffic.points[0] = {
      ts: point.traffic.points[0]!.ts,
      request_mbps: 1,
      response_mbps: 2,
    } as never
    expect(() => parseSystemStatus(point)).toThrow('Invalid API response')
  })
  it('rejects Root-only diagnostics in place of the projection', () => {
    const { cpu, ...raw } = payload()
    Object.assign(raw, { resources: { cpu } })
    expect(() => parseSystemStatus(raw)).toThrow('Invalid API response')
  })
})
