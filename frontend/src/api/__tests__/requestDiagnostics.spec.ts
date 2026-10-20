import { describe, expect, it } from 'vitest'
import { parseRequestDiagnostic } from '../requestDiagnostics'

const diagnostic = {
  schema_version: 1,
  request_id: 'req-1',
  token_id: 7,
  model_name: 'model',
  created_at: 1700000000,
  wait_ms: 19,
  user_queue_ms: 7,
  attempts: [
    {
      sequence: 1,
      attempt_id: 'attempt-1',
      success: false,
      error_class: 'rate_limit',
      error_http_status: 429,
      origin: 'upstream',
      latency_ms: 120,
      queue_ms: 12,
      send_started_at: 1700000000000,
      finished_at: 1700000000120,
      acceptance: 'rejected',
      retry_permission: 'allowed',
      response_committed: false,
      channel_id: 12,
      credential_id: 'credential-private',
    },
  ],
}

describe('request diagnostics boundary', () => {
  it('strips administrator identities and unknown body fields from self projection', () => {
    const parsed = parseRequestDiagnostic({
      ...diagnostic,
      body: 'never-forward',
      attempts: [{ ...diagnostic.attempts[0], response_body: 'never-forward' }],
    })
    expect(parsed.attempts[0]).not.toHaveProperty('channel_id')
    expect(parsed.attempts[0]).not.toHaveProperty('credential_id')
    expect(JSON.stringify(parsed)).not.toContain('never-forward')
    expect(parsed.attempts[0]?.first_output_at).toBeUndefined()
    expect(parsed.attempts[0]?.ttft_ms).toBeUndefined()
    expect(parsed.attempts[0]?.upstream_latency_ms).toBeUndefined()
    expect(parseRequestDiagnostic(diagnostic, true).attempts[0]).toMatchObject({
      channel_id: 12,
      credential_id: 'credential-private',
    })
  })
  it('rejects malformed timing and missing attempt facts instead of inventing a timeline', () => {
    expect(() =>
      parseRequestDiagnostic({
        ...diagnostic,
        attempts: [{ ...diagnostic.attempts[0], upstream_latency_ms: -1 }],
      })
    ).toThrow()
    expect(() =>
      parseRequestDiagnostic({ ...diagnostic, wait_ms: -1 })
    ).toThrow()
    expect(() =>
      parseRequestDiagnostic({ ...diagnostic, attempts: [{}] })
    ).toThrow()
    expect(() =>
      parseRequestDiagnostic({
        ...diagnostic,
        attempts: [{ ...diagnostic.attempts[0], success: 'true' }],
      })
    ).toThrow()
    expect(() =>
      parseRequestDiagnostic({ ...diagnostic, schema_version: 2 })
    ).toThrow()
  })
  it('keeps measured upstream duration separate from preparation-inclusive attempt duration', () => {
    const parsed = parseRequestDiagnostic({
      ...diagnostic,
      attempts: [{ ...diagnostic.attempts[0], upstream_latency_ms: 0 }],
    })
    expect(parsed.attempts[0]?.latency_ms).toBe(120)
    expect(parsed.attempts[0]?.upstream_latency_ms).toBe(0)
    const historical = parseRequestDiagnostic({
      ...diagnostic,
      attempts: [{ ...diagnostic.attempts[0], upstream_latency_ms: null }],
    })
    expect(historical.attempts[0]?.upstream_latency_ms).toBeUndefined()
  })
})
