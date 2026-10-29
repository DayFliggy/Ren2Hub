import type {
  CacheWriteMode,
  LegacyBillingDetails,
  PricingValues,
} from '@/api/pricingConfig'
import { formatUnitPriceUSD, multiplyDecimal } from '@/utils/decimal'
import { decimalText, pricingMode, tokenPrices } from './draft'

// types/price_data.go: ClaudeCacheCreation1hMultiplier = 6 / 3.75 and
// OpenAICacheCreation12hMultiplier = 2.
const CLAUDE_1H_MULTIPLIER = '1.6'
const OPENAI_12H_MULTIPLIER = '2'

export type PreviewUnit = 'perMillion' | 'perRequest' | 'perImage' | 'none'
// label is a message key, except for request-rule conditions, which the
// backend reports as source text.
export interface PreviewRow {
  key: string
  label: string
  value: string
  unit: PreviewUnit
  translateLabel: boolean
}

function row(key: string, value: string, unit: PreviewUnit): PreviewRow {
  return { key, label: key, value, unit, translateLabel: true }
}
function price(value: string): string {
  return formatUnitPriceUSD(value)
}
// A ratio of 1 is the default and adds no separate price.
function priced(values: PricingValues, key: keyof PricingValues): boolean {
  const ratio = values[key]
  return ratio !== undefined && decimalText(String(ratio)) !== '1'
}

function tokenRows(
  values: PricingValues,
  cacheWriteMode: CacheWriteMode | undefined,
  details: LegacyBillingDetails | undefined
): PreviewRow[] {
  const prices = tokenPrices(values)
  if (!prices.input) return []
  const rows = [row('input', price(prices.input), 'perMillion')]
  if (prices.CompletionRatio)
    rows.push(row('output', price(prices.CompletionRatio), 'perMillion'))
  if (priced(values, 'CacheRatio') && prices.CacheRatio)
    rows.push(row('cache', price(prices.CacheRatio), 'perMillion'))
  const write = prices.CreateCacheRatio
  const showWrite = cacheWriteMode
    ? cacheWriteMode !== 'none'
    : values.CreateCacheRatio !== undefined
  if (showWrite && write) {
    if (cacheWriteMode === 'claude_ttl') {
      rows.push(row('cacheWrite5m', price(write), 'perMillion'))
      rows.push(
        row(
          'cacheWrite1h',
          price(multiplyDecimal(write, CLAUDE_1H_MULTIPLIER)),
          'perMillion'
        )
      )
    } else if (cacheWriteMode === 'openai_ttl') {
      rows.push(row('cacheWrite30m', price(write), 'perMillion'))
      rows.push(
        row(
          'cacheWrite12h',
          price(multiplyDecimal(write, OPENAI_12H_MULTIPLIER)),
          'perMillion'
        )
      )
    } else rows.push(row('cacheWrite', price(write), 'perMillion'))
  }
  if (priced(values, 'ImageRatio') && prices.ImageRatio)
    rows.push(row('image', price(prices.ImageRatio), 'perMillion'))
  // Gemini audio has an absolute price the ratios cannot always express.
  const audio =
    details?.audio_input_price === undefined
      ? prices.AudioRatio
      : decimalText(details.audio_input_price)
  const audioOutput =
    details?.audio_output_price === undefined
      ? prices.AudioCompletionRatio
      : decimalText(details.audio_output_price)
  if (audio) rows.push(row('audio', price(audio), 'perMillion'))
  if (audioOutput)
    rows.push(row('audioOutput', price(audioOutput), 'perMillion'))
  return rows
}

function adjustmentRows(details: LegacyBillingDetails | undefined) {
  const rows: PreviewRow[] = []
  if (details?.audio_text_branches)
    rows.push(row('audioTextBranches', '', 'none'))
  if (details?.image_count) rows.push(row('imageCountNote', '', 'none'))
  for (const [index, rule] of (details?.request_rules ?? []).entries())
    rows.push({
      key: `rule-${index}`,
      label: rule.condition,
      value: `× ${rule.multiplier}`,
      unit: 'none',
      translateLabel: false,
    })
  return rows
}

// Rows describe one model's prices in USD, as NewAPI's pricing preview does.
// cacheWriteMode and details come from the backend description; a list row
// without them still gets the configured prices.
export function pricingPreviewRows(
  values: PricingValues,
  cacheWriteMode?: CacheWriteMode,
  details?: LegacyBillingDetails
): PreviewRow[] {
  if (!Object.keys(values).length) return []
  const mode = pricingMode(values)
  if (mode === 'tiered_expr') {
    const expression = String(values['billing_setting.billing_expr'] ?? '')
    return expression ? [row('expression', expression, 'none')] : []
  }
  if (mode === 'per-request') {
    const fixed = decimalText(String(values.ModelPrice))
    const rows = details?.image_count
      ? [row('imagePrice', price(fixed), 'perImage')]
      : [row('fixed', price(fixed), 'perRequest')]
    return [...rows, ...adjustmentRows(details)]
  }
  return [
    ...tokenRows(values, cacheWriteMode, details),
    ...adjustmentRows(details),
  ]
}
