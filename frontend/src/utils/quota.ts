import { shallowRef } from 'vue'
import { divideDecimal, multiplyDecimal } from './decimal'

/** Exact display arithmetic. Native numeric API values must be safe integers. */
export type Quota = string
export const QUOTA_PER_USD = 500_000n
export const MAX_QUOTA = 9_223_372_036_854_775_807n
export const MIN_QUOTA = -9_223_372_036_854_775_808n

const currentQuotaPerUnit = shallowRef<bigint | number>(QUOTA_PER_USD)

/** The backend publishes this scale in /api/status. */
export function configureQuotaPerUnit(value: number): void {
  if (!Number.isFinite(value) || value <= 0)
    throw new RangeError('Invalid quota_per_unit')
  currentQuotaPerUnit.value = value
}

export function getQuotaPerUnit(): bigint | number {
  return currentQuotaPerUnit.value
}

/** Native model ratios are quota per token; the editor shows USD per million. */
export function modelRatioToPrice(ratio: string): string {
  const unit = quotaUnit(currentQuotaPerUnit.value)
  return divideDecimal(
    multiplyDecimal(ratio, (1_000_000n * unit.denominator).toString()),
    unit.numerator.toString()
  )
}

export function priceToModelRatio(price: string): string {
  const unit = quotaUnit(currentQuotaPerUnit.value)
  return divideDecimal(
    multiplyDecimal(price, unit.numerator.toString()),
    (1_000_000n * unit.denominator).toString()
  )
}

function quotaUnit(value: bigint | number): {
  numerator: bigint
  denominator: bigint
} {
  if (typeof value === 'bigint') {
    if (value <= 0n) throw new RangeError('Invalid quota_per_unit')
    return { numerator: value, denominator: 1n }
  }
  if (!Number.isFinite(value) || value <= 0)
    throw new RangeError('Invalid quota_per_unit')
  // Use the wire number's decimal value, including exponent notation, without
  // introducing a floating-point multiplication into editable quota amounts.
  const [coefficient = '', exponent = '0'] = value.toString().split('e')
  const [whole = '', fraction = ''] = coefficient.split('.')
  const power = Number(exponent) - fraction.length
  const digits = BigInt(whole + fraction)
  return power >= 0
    ? { numerator: digits * 10n ** BigInt(power), denominator: 1n }
    : { numerator: digits, denominator: 10n ** BigInt(-power) }
}

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
  digits = 6,
  perUnit: bigint | number = currentQuotaPerUnit.value
): string {
  if (!Number.isInteger(digits) || digits < 0 || digits > 12)
    throw new RangeError('Invalid precision')
  const quota = quotaInteger(value)
  const negative = quota < 0n
  const absolute = negative ? -quota : quota
  const scale = 10n ** BigInt(digits)
  const unit = quotaUnit(perUnit)
  const rounded =
    (absolute * scale * unit.denominator + unit.numerator / 2n) / unit.numerator
  const whole = rounded / scale
  const fraction = (rounded % scale).toString().padStart(digits, '0')
  return `${negative ? '-' : ''}${whole}${digits ? `.${fraction}` : ''}`
}

/** Reject fractional quota and overflow instead of silently rounding an input. */
export function decimalToQuota(
  value: string,
  perUnit: bigint | number = currentQuotaPerUnit.value
): Quota {
  if (!/^-?\d+(?:\.\d{1,6})?$/.test(value))
    throw new RangeError('Invalid amount')
  const negative = value.startsWith('-')
  const [whole = '0', fraction = ''] = value.replace(/^-/, '').split('.')
  const millionths =
    BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, '0'))
  const unit = quotaUnit(perUnit)
  const scaled = millionths * unit.numerator
  const divisor = 1_000_000n * unit.denominator
  if (scaled % divisor !== 0n) throw new RangeError('Fractional quota')
  const quota = (scaled / divisor) * (negative ? -1n : 1n)
  if (quota < MIN_QUOTA || quota > MAX_QUOTA)
    throw new RangeError('Quota overflow')
  return quota.toString()
}

/** Lossy projection is permitted only at the chart rendering boundary. */
export function quotaForChart(
  value: Quota,
  perUnit: bigint | number = currentQuotaPerUnit.value
): number {
  return Number(quotaInteger(value)) / Number(perUnit)
}
