import type { SystemResource, SystemStatus } from '@/api/systemStatus'
const gib = 1024 ** 3
export function resourceFixture(
  changes: Partial<SystemResource> = {}
): SystemResource {
  return {
    available: true,
    state: 'ok',
    scope: 'container',
    percent: 34,
    used_bytes: null,
    total_bytes: null,
    ...changes,
  }
}
export function systemStatusFixture(): SystemStatus {
  return {
    status: 'ok',
    stale: false,
    sampled_at: 1_798_540_000,
    cpu: resourceFixture(),
    memory: resourceFixture({
      percent: 32.5,
      used_bytes: 5.2 * gib,
      total_bytes: 16 * gib,
    }),
    disk: resourceFixture({
      percent: 42.6,
      scope: 'filesystem',
      used_bytes: 218 * gib,
      total_bytes: 512 * gib,
    }),
    load: { scope: 'host', one: 1.2, five: 0.84, fifteen: 0.5, cores: 4 },
    traffic: {
      scope: 'application_http_body',
      request_mbps: 2.1,
      response_mbps: 12.4,
      bucket_seconds: 60,
      points: [0, 1, 2].map((i) => ({
        ts: 1_798_539_880 + i * 60,
        request_mbps: 1.1 + i * 0.5,
        response_mbps: 8.6 + i * 1.9,
      })),
    },
    success_rate: {
      state: 'ok',
      rate: 99.4,
      window_start: 1_798_459_200,
      window_end: 1_798_540_000,
      checked_at: 1_798_540_000,
    },
  }
}
