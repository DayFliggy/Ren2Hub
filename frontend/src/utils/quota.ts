/** Exact display arithmetic. Native numeric API values must be safe integers. */
export type Quota = string
export const QUOTA_PER_USD = 500_000n
export const MAX_QUOTA = 9_223_372_036_854_775_807n
export const MIN_QUOTA = -9_223_372_036_854_775_808n

export function isQuota(value: unknown): value is Quota {
  if (typeof value !== 'string' || !/^(?:0|-?[1-9]\d*)$/.test(value))
    return false
  const amount = BigInt(value)
  return amount >= MIN_QUOTA && amount <= MAX_QUOTA
}

export function quotaInteger(value: Quota | bigint | number): bigint {
  if (typeof value === 'bigint') return value
  if (typeof value === 'number') {
    if (!Number.isSafeInteger(value)) throw new RangeError('Invalid quota')
    return BigInt(value)
  }
  if (!isQuota(value)) throw new RangeError('Invalid quota')
  return BigInt(value)
}

export function addQuota(a: Quota, b: Quota): Quota {
  const sum = quotaInteger(a) + quotaInteger(b)
  if (sum < MIN_QUOTA || sum > MAX_QUOTA) throw new RangeError('Quota overflow')
  return sum.toString()
}

export function sumQuota(values: Iterable<Quota>): Quota {
  let sum = '0'
  for (const value of values) sum = addQuota(sum, value)
  return sum
}

/** Display ratio only; divide as integers before converting the bounded result. */
export function quotaRatio(
  numerator: Quota | bigint,
  denominator: Quota | bigint
): number {
  const divisor = quotaInteger(denominator)
  if (divisor <= 0n) return 0
  return Number((quotaInteger(numerator) * 1_000_000n) / divisor) / 1_000_000
}

export function quotaToDecimal(
  value: Quota | bigint | number,
  digits = 6
): string {
  if (!Number.isInteger(digits) || digits < 0 || digits > 12)
    throw new RangeError('Invalid precision')
  const quota = quotaInteger(value)
  const negative = quota < 0n
  const absolute = negative ? -quota : quota
  const scale = 10n ** BigInt(digits)
  const rounded = (absolute * scale + QUOTA_PER_USD / 2n) / QUOTA_PER_USD
  const whole = rounded / scale
  const fraction = (rounded % scale).toString().padStart(digits, '0')
  return `${negative ? '-' : ''}${whole}${digits ? `.${fraction}` : ''}`
}

/** Reject fractional quota and overflow instead of silently rounding an input. */
export function decimalToQuota(value: string): Quota {
  if (!/^-?\d+(?:\.\d{1,6})?$/.test(value))
    throw new RangeError('Invalid amount')
  const negative = value.startsWith('-')
  const [whole = '0', fraction = ''] = value.replace(/^-/, '').split('.')
  const millionths =
    BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, '0'))
  if (millionths % 2n !== 0n) throw new RangeError('Fractional quota')
  const quota = (millionths / 2n) * (negative ? -1n : 1n)
  if (quota < MIN_QUOTA || quota > MAX_QUOTA)
    throw new RangeError('Quota overflow')
  return quota.toString()
}

/** Lossy projection is permitted only at the chart rendering boundary. */
export function quotaForChart(value: Quota): number {
  return Number(quotaInteger(value)) / Number(QUOTA_PER_USD)
}
