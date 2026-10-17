import { describe, expect, it, vi } from 'vitest'
import {
  parseConversionPolicy,
  parseGatewayAdmission,
  parseGatewayConfig,
  parseGatewayOverride,
  parseGatewayRuntime,
  gatewayApi,
} from '@/api/gateway'

const get = vi.hoisted(() => vi.fn())
vi.mock('@/api/console', () => ({ api: { get } }))

const config = {
  total_queue_size: 1024,
  retry_budget_enabled: true,
  retry_budget_percent: 10,
  retry_budget_burst: 20,
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
  it('requires all new configuration fields and enforces their exact ranges', () => {
    for (const field of [
      'total_queue_size',
      'retry_budget_enabled',
      'retry_budget_percent',
      'retry_budget_burst',
    ]) {
      const missing: Record<string, unknown> = { ...config }
      delete missing[field]
      expect(() => parseGatewayConfig(missing)).toThrow()
      expect(() => parseGatewayConfig({ ...config, [field]: null })).toThrow()
    }
    for (const invalid of [
      { total_queue_size: -1 },
      { total_queue_size: 100001 },
      { retry_budget_percent: 0 },
      { retry_budget_percent: 101 },
      { retry_budget_burst: 0 },
      { retry_budget_burst: 10001 },
      { retry_budget_enabled: 0 },
    ])
      expect(() => parseGatewayConfig({ ...config, ...invalid })).toThrow()
    expect(
      parseGatewayConfig({
        ...config,
        total_queue_size: 0,
        retry_budget_enabled: false,
      })
    ).toMatchObject({ total_queue_size: 0, retry_budget_enabled: false })
  })

  it('preserves local runtime when provider state is unavailable and rejects ambiguous state', async () => {
    const runtime = {
      in_flight: 3,
      queued: 2,
      capacity: 20,
      config_version: 7,
      revision: 12,
      started_at: 1700000000,
      draining: true,
    }
    for (const field of Object.keys(runtime)) {
      const missing: Record<string, unknown> = { ...runtime }
      delete missing[field]
      expect(() => parseGatewayRuntime(missing)).toThrow()
      expect(() => parseGatewayRuntime({ ...runtime, [field]: null })).toThrow()
    }
    const partial = {
      ...runtime,
      channel_id: 12,
      rate: null,
      rate_status: 'unavailable',
    }
    get.mockResolvedValue(partial)
    await expect(gatewayApi.channelRuntime(12)).resolves.toEqual(partial)
    for (const invalid of [
      { rate_status: 'available' },
      { rate_status: undefined },
      { rate: undefined },
      {
        rate: {
          rpm_used: 0,
          tokens_used: 0,
          tokens_reserved: 0,
          cooldown_ms: 0,
        },
      },
    ]) {
      get.mockResolvedValue({ ...partial, ...invalid })
      await expect(gatewayApi.channelRuntime(12)).rejects.toThrow()
    }
  })
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
