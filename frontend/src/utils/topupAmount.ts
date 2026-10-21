import { paymentProvider, type TopupInfo } from '@/api/topup'

export interface TopupInputAmounts {
  minimum: number
  step: number
  presets: number[]
  tokens: boolean
  scale: number
}

/** Presets are configured in recharge units; token-mode requests use quota units. */
export function topupInputAmounts(
  info: TopupInfo,
  method: string,
  status: { quota_display_type?: string; quota_per_unit?: number }
): TopupInputAmounts | null {
  const tokens = status.quota_display_type === 'TOKENS'
  const scale = tokens ? status.quota_per_unit : 1
  if (typeof scale !== 'number' || !Number.isSafeInteger(scale) || scale <= 0)
    return null
  const provider = paymentProvider(method)
  // Waffo endpoints compare their configured minimum directly with the request;
  // EPay/GMPay and Stripe scale that minimum in token display mode.
  const rawMinimum =
    provider === 'stripe'
      ? info.stripe_min_topup
      : provider === 'waffo'
        ? info.waffo_min_topup
        : provider === 'waffo_pancake'
          ? info.waffo_pancake_min_topup
          : info.min_topup
  const serverMinimum = Math.max(
    1,
    rawMinimum *
      (provider === 'waffo' || provider === 'waffo_pancake' ? 1 : scale)
  )
  // EPay/GMPay, Waffo and Pancake validate through getTopUpQuota, which
  // truncates token quantities to whole recharge units. Keep UI inputs exact.
  const step = scale
  const minimum = Math.ceil(serverMinimum / step) * step
  const presets = info.amount_options.map((value) => value * scale)
  if (
    !Number.isSafeInteger(minimum) ||
    presets.some((value) => !Number.isSafeInteger(value) || value <= 0)
  )
    return null
  return { minimum, step, presets, tokens, scale }
}

export function validTopupAmount(
  value: number,
  amounts: TopupInputAmounts | null
): boolean {
  return (
    amounts !== null &&
    Number.isSafeInteger(value) &&
    value >= amounts.minimum &&
    value % amounts.step === 0
  )
}
