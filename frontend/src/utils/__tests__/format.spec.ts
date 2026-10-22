import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  configureQuotaDisplay,
  formatBytes,
  formatCompact,
  formatDuration,
  formatLatency,
  formatMoney,
  formatQuota,
  formatQuotaParts,
  formatTokenPrice,
  localDayEndMs,
  localDayStartMs,
  maskKey,
  passwordStrength,
  relativeTime,
  fromDateTimeLocal,
  toDateTimeLocal,
} from '@/utils/format'

describe('maskKey', () => {
  it('never returns a non-empty secret verbatim', () => {
    expect(maskKey('')).toBe('')
    expect(maskKey('a')).toBe('•')
    expect(maskKey('sk-abcdefgh')).toBe('sk-••••gh')
    expect(maskKey('secret')).not.toBe('secret')
    expect(maskKey('secret')).not.toContain('secret')
  })

  it('keeps the established preview shape for normal token keys', () => {
    expect(maskKey('sk-7YX12345678lpK')).toBe('sk-7YX••••8lpK')
  })
})

describe('quota and money formatting', () => {
  it('converts quota to dollars at 500k per dollar', () => {
    expect(formatQuota(500_000)).toBe('$1.00')
    expect(formatQuota(1_250_000, 4)).toBe('$2.500')
    expect(formatQuota(0)).toBe('$0.00')
  })

  it('formats money with grouping', () => {
    expect(formatMoney(1234.5)).toBe('$1,234.50')
  })

  describe('quota parts for each display mode', () => {
    afterEach(() => configureQuotaDisplay({}))
    const cases = [
      {
        status: { quota_display_type: 'USD' },
        parts: { prefix: '$', whole: '1,234', fraction: '57' },
        text: '$1,234.57',
      },
      {
        status: { quota_display_type: 'USD', quota_per_unit: 1_000_000 },
        parts: { prefix: '$', whole: '617', fraction: '28' },
        text: '$617.28',
      },
      {
        status: { quota_display_type: 'CNY', usd_exchange_rate: 7.3 },
        parts: { prefix: '¥', whole: '9,012', fraction: '35' },
        text: '¥9,012.35',
      },
      {
        status: {
          quota_display_type: 'CUSTOM',
          custom_currency_symbol: 'Pts ',
          custom_currency_exchange_rate: 10,
        },
        parts: { prefix: 'Pts ', whole: '12,345', fraction: '68' },
        text: 'Pts 12,345.68',
      },
      {
        status: { quota_display_type: 'TOKENS' },
        parts: { prefix: '', whole: '617,283,950', fraction: '' },
        text: '617,283,950',
      },
    ]
    it.each(cases)('$status.quota_display_type splits $text', (item) => {
      configureQuotaDisplay(item.status)
      expect(formatQuotaParts(617_283_950)).toEqual(item.parts)
      expect(formatQuota(617_283_950)).toBe(item.text)
    })

    it('drops the decimal part at zero digits', () => {
      expect(formatQuotaParts(617_283_950, 0)).toEqual({
        prefix: '$',
        whole: '1,235',
        fraction: '',
      })
      expect(formatQuota(617_283_950, 0)).toBe('$1,235')
    })
  })

  it('formats token prices and missing values', () => {
    expect(formatTokenPrice('1.25')).toBe('$1.250')
    expect(formatTokenPrice(undefined)).toBe('—')
  })
})

describe('compact numeric labels', () => {
  it('abbreviates thousands and millions', () => {
    expect(formatCompact(999)).toBe('999')
    expect(formatCompact(1_000)).toBe('1.0K')
    expect(formatCompact(1_500_000)).toBe('1.5M')
  })

  it('labels byte sizes', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(12_800)).toBe('12.5 KB')
    expect(formatBytes(2_205_000)).toBe('2.1 MB')
  })

  it('labels durations and latency', () => {
    expect(formatDuration(5)).toBe('5s')
    expect(formatDuration(75)).toBe('1:15')
    expect(formatLatency(1.784)).toBe('1.78s')
    expect(formatLatency(18.02)).toBe('18.0s')
  })
})

describe('relativeTime', () => {
  it('localizes recent and older timestamps', () => {
    const now = Math.floor(Date.now() / 1000)
    expect(relativeTime(now - 30, 'zh-CN')).toBe('刚刚')
    expect(relativeTime(now - 30, 'en')).toBe('just now')
    expect(relativeTime(now - 120, 'en')).toBe('2m ago')
    expect(relativeTime(now - 7_200, 'zh-CN')).toBe('2 小时前')
    expect(relativeTime(now - 3 * 86_400, 'en')).toBe('3d ago')
  })
})

describe('passwordStrength', () => {
  it('scores from empty to strong', () => {
    expect(passwordStrength('')).toBe(0)
    expect(passwordStrength('abc')).toBe(1)
    expect(passwordStrength('Abcdef12')).toBe(2)
    expect(passwordStrength('Abcdef12345!')).toBe(3)
  })
})

describe('local calendar day bounds', () => {
  // Node re-reads the zone whenever the TZ variable is assigned.
  afterEach(() => {
    vi.unstubAllEnvs()
  })

  it('uses local midnight instead of UTC midnight', () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    expect(localDayStartMs('2026-10-01') / 1000).toBe(1790784000)
    expect(Math.floor(localDayEndMs('2026-10-01') / 1000)).toBe(1790870399)
  })

  it('keeps daylight-saving days exact', () => {
    vi.stubEnv('TZ', 'America/New_York')
    const dayLength = (day: string) =>
      localDayEndMs(day) - localDayStartMs(day) + 1
    expect(dayLength('2026-11-01')).toBe(25 * 3_600_000)
    expect(dayLength('2026-03-08')).toBe(23 * 3_600_000)
  })

  it('ends a day whose midnight is skipped at that day, not an hour later', () => {
    // Chile starts daylight saving at 00:00 on 2026-09-06, so that day begins at 01:00.
    vi.stubEnv('TZ', 'America/Santiago')
    expect(localDayEndMs('2026-09-06')).toBe(1788749999999)
    expect(
      localDayEndMs('2026-09-06') - localDayStartMs('2026-09-06') + 1
    ).toBe(23 * 3_600_000)
  })

  it('rejects anything but a real YYYY-MM-DD date', () => {
    for (const value of ['', '2026-1-01', '2026-02-30', '2026-10-01T00:00']) {
      expect(() => localDayStartMs(value)).toThrow()
      expect(() => localDayEndMs(value)).toThrow()
    }
  })
})

describe('datetime-local conversion', () => {
  afterEach(() => vi.unstubAllEnvs())

  it('round-trips a local instant to the second', () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    // 2026-10-12 23:00:30 in UTC+8.
    const epoch = Date.UTC(2026, 9, 12, 15, 0, 30) / 1000
    expect(toDateTimeLocal(epoch)).toBe('2026-10-12T23:00:30')
    expect(fromDateTimeLocal('2026-10-12T23:00:30')).toBe(epoch)
    // Browsers drop ':00' seconds even with step=1; fractions are truncated.
    expect(fromDateTimeLocal('2026-10-12T23:00')).toBe(epoch - 30)
    expect(fromDateTimeLocal('2026-10-12T23:00:30.900')).toBe(epoch)
  })

  it('rejects text that is not a real local date and time', () => {
    vi.stubEnv('TZ', 'America/New_York')
    for (const value of [
      '',
      '2026-10-12',
      '2026-02-30T10:00:00',
      '2026-10-12T24:00:00',
      '2026-10-12 10:00:00',
      // Skipped by the 2026-03-08 spring-forward jump.
      '2026-03-08T02:30:00',
    ]) {
      expect(fromDateTimeLocal(value)).toBeNull()
    }
  })
})
