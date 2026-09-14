import { describe, expect, it } from 'vitest'
import {
  parseConversionPolicy,
  parseGatewayAdmission,
  parseGatewayConfig,
  parseGatewayOverride,
  parseGatewayRuntime,
} from '@/api/gateway'

const config = {
  version: 1,
  user_concurrency: 10,
  wait_ms: 5000,
  user_queue_size: 20,
  channel_queue_size: 20,
  groups: {},
  channel_rates: {},
  credential_rates: {},
}

describe('gateway contracts', () => {
  it('preserves explicit zero and nullable inheritance without coercion', () => {
    expect(
      parseGatewayOverride({ concurrency: 0, wait_ms: null, queue_size: 0 })
    ).toEqual({ concurrency: 0, wait_ms: null, queue_size: 0 })
    expect(() =>
      parseGatewayOverride({ concurrency: '0', wait_ms: null, queue_size: 0 })
    ).toThrow()
    expect(() =>
      parseGatewayOverride({ concurrency: 0, queue_size: 0 })
    ).toThrow()
    expect(() =>
      parseGatewayOverride({ concurrency: 0, wait_ms: 5001, queue_size: 0 })
    ).toThrow()
    expect(() =>
      parseGatewayOverride({ concurrency: null, wait_ms: null, queue_size: 21 })
    ).toThrow()
    expect(() => parseGatewayConfig({ ...config, wait_ms: 5001 })).toThrow()
    expect(() =>
      parseGatewayConfig({ ...config, user_queue_size: 21 })
    ).toThrow()
  })

  it('rejects malformed rate identities, invalid limits and unknown sources', () => {
    const valid = {
      ...config,
      channel_rates: { '12': { rpm: 60, tpm: 9000 } },
      credential_rates: {
        '6a302bce-9ba5-49af-aef0-afcf5971fc16': { rpm: 0, tpm: 2000 },
      },
    }
    expect(parseGatewayConfig(valid)).toEqual(valid)
    expect(() =>
      parseGatewayConfig({
        ...config,
        channel_rates: { '0': { rpm: 1, tpm: 1 } },
      })
    ).toThrow()
    expect(() =>
      parseGatewayConfig({
        ...config,
        credential_rates: { 'key-index-0': { rpm: 1, tpm: 1 } },
      })
    ).toThrow()
    expect(() =>
      parseGatewayConfig({
        ...config,
        channel_rates: { '12': { rpm: 1, tpm: -1 } },
      })
    ).toThrow()
    expect(() => parseGatewayRuntime({ in_flight: '0', queued: 0 })).toThrow()
    expect(() =>
      parseGatewayAdmission({
        user_id: 1,
        group: 'team',
        config_version: 1,
        override: { concurrency: null, wait_ms: null, queue_size: null },
        effective: {
          concurrency: 10,
          wait_ms: 5000,
          queue_size: 20,
          channel_queue_size: 20,
        },
        sources: {
          concurrency: 'unknown',
          wait_ms: 'system',
          queue_size: 'system',
          channel_queue_size: 'system',
        },
      })
    ).toThrow()
  })

  it('accepts only the supported token conversion policies', () => {
    for (const policy of ['safe', 'allow', 'strict'])
      expect(parseConversionPolicy(policy)).toBe(policy)
    expect(() => parseConversionPolicy('automatic')).toThrow()
  })
})
