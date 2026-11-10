import { THRESHOLD_PATTERN } from '@/api/topupDiscount'
import { shiftDecimal } from '@/utils/decimal'
import { decimalToQuota, quotaToDecimal } from '@/utils/quota'

// Display conversions for the editor. Thresholds travel as integer quota
// strings and are edited as USD, using the site's quota_per_unit (configured
// from /api/status); BigInt arithmetic keeps values beyond
// Number.MAX_SAFE_INTEGER exact. Rates travel as decimal fractions and are
// shown as percents, like the other console discounts.

const AMOUNT_PATTERN = /^\d+(?:\.\d{1,6})?$/

/** USD text for a quota threshold, without trailing zeros. */
export function quotaToAmount(quota: string): string {
  return quotaToDecimal(quota, 6).replace(/\.?0+$/, '') || '0'
}

/**
 * The quota threshold for USD text. Throws an Error whose message is an i18n
 * key: the amount is malformed, is not a whole quota, or exceeds the 18 digits
 * the server accepts.
 */
export function amountToQuota(amount: string): string {
  const text = amount.trim()
  if (!AMOUNT_PATTERN.test(text)) throw new Error('errors.amountInvalid')
  let quota: string
  try {
    quota = decimalToQuota(text)
  } catch (cause) {
    if (cause instanceof RangeError && cause.message === 'Fractional quota')
      throw new Error('errors.amountFractional', { cause })
    throw new Error('errors.amountTooLarge', { cause })
  }
  if (!THRESHOLD_PATTERN.test(quota)) throw new Error('errors.amountTooLarge')
  return quota
}

/** "0.0125" → "1.25"; exact decimal shift, no floating point. */
export function rateToPercent(rate: string): string {
  return shiftDecimal(rate, -2)
}

/** "1.25" → "0.0125"; the caller validates the percent text first. */
export function percentToRate(percent: string): string {
  return shiftDecimal(percent, 2)
}
