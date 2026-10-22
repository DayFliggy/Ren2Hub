import { expect, it } from 'vitest'
import { nativeUsageSummary } from '../../../tests/visual/channel-core-fixtures'

it.each(['2026-07-27T12:00:00+08:00', '2026-07-27T12:23:45+08:00'])(
  'keeps usage fixture hours aligned and inside [start,end) at %s',
  (date) => {
    const now = new Date(date).getTime() / 1000
    const { hourly, meta } = nativeUsageSummary(now)

    expect(hourly).toHaveLength(24)
    for (const [index, hour] of hourly.entries()) {
      expect(hour.ts % meta.bucket_seconds).toBe(0)
      expect(hour.ts).toBeGreaterThanOrEqual(meta.start)
      expect(hour.ts).toBeLessThan(meta.end)
      if (index > 0) {
        expect(hour.ts - hourly[index - 1]!.ts).toBe(meta.bucket_seconds)
      }
    }
    expect(meta.end - hourly.at(-1)!.ts).toBeLessThanOrEqual(
      meta.bucket_seconds
    )
  }
)
