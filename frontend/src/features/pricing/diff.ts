import {
  PRICING_KEYS,
  serializePricing,
  type PricingValues,
} from '@/api/pricingConfig'

export interface PriceDiffSegment {
  text: string
  changed: boolean
}
export interface PriceDiffField {
  key: string
  change: 'unchanged' | 'added' | 'removed' | 'changed'
  current: PriceDiffSegment[] | null
  candidate: PriceDiffSegment[] | null
}

/** Compare source text without coercing prices or rounding expression literals. */
export function expressionDiff(
  before: string,
  after: string
): [PriceDiffSegment[], PriceDiffSegment[]] {
  const tokenize = (text: string) =>
    text.match(/\s+|[\p{L}\p{N}_]+(?:\.[\p{L}\p{N}_]+)*|[^\s]/gu) ?? []
  const left = tokenize(before),
    right = tokenize(after)
  const output: [PriceDiffSegment[], PriceDiffSegment[]] = [[], []]
  function append(side: 0 | 1, text: string, changed: boolean) {
    if (!text) return
    const last = output[side].at(-1)
    if (last?.changed === changed) last.text += text
    else output[side].push({ text, changed })
  }
  // Bound comparison work for large expressions while retaining their exact text.
  if (left.length * right.length > 160_000) {
    let start = 0
    while (
      start < left.length &&
      start < right.length &&
      left[start] === right[start]
    )
      start++
    let end = 0
    while (
      end < left.length - start &&
      end < right.length - start &&
      left[left.length - end - 1] === right[right.length - end - 1]
    )
      end++
    for (const side of [0, 1] as const) {
      const tokens = side === 0 ? left : right
      append(side, tokens.slice(0, start).join(''), false)
      append(side, tokens.slice(start, tokens.length - end).join(''), true)
      append(side, tokens.slice(tokens.length - end).join(''), false)
    }
    return output
  }
  const lengths = Array.from(
    { length: left.length + 1 },
    () => new Uint16Array(right.length + 1)
  )
  for (let i = left.length - 1; i >= 0; i--)
    for (let j = right.length - 1; j >= 0; j--)
      lengths[i]![j] =
        left[i] === right[j]
          ? lengths[i + 1]![j + 1]! + 1
          : Math.max(lengths[i + 1]![j]!, lengths[i]![j + 1]!)
  let i = 0,
    j = 0
  while (i < left.length || j < right.length) {
    if (i < left.length && j < right.length && left[i] === right[j]) {
      append(0, left[i++]!, false)
      append(1, right[j++]!, false)
    } else if (
      i < left.length &&
      (j === right.length || lengths[i + 1]![j]! >= lengths[i]![j + 1]!)
    ) {
      append(0, left[i++]!, true)
    } else append(1, right[j++]!, true)
  }
  return output
}

export function pricingDiff(
  current: PricingValues,
  candidate: PricingValues
): PriceDiffField[] {
  const fields: PriceDiffField[] = []
  function add(
    key: string,
    before: string | number | undefined,
    after: string | number | undefined
  ) {
    const change =
      before === undefined
        ? 'added'
        : after === undefined
          ? 'removed'
          : before === after
            ? 'unchanged'
            : 'changed'
    const [left, right] = expressionDiff(
      before === undefined ? '' : String(before),
      after === undefined ? '' : String(after)
    )
    fields.push({
      key,
      change,
      current: before === undefined ? null : left,
      candidate: after === undefined ? null : right,
    })
  }
  // Draft decimal text and fetched numbers compare as the serialized request.
  const serializedCurrent = serializePricing(current),
    serializedCandidate = serializePricing(candidate)
  for (const key of PRICING_KEYS) {
    if (!(key in current) && !(key in candidate)) continue
    if (key === 'billing_setting.plugin_billing_expr') {
      const before = current[key] ?? {},
        after = candidate[key] ?? {}
      for (const plugin of [
        ...new Set([...Object.keys(before), ...Object.keys(after)]),
      ].sort())
        add(`${key}.${plugin}`, before[plugin], after[plugin])
    } else add(key, serializedCurrent[key], serializedCandidate[key])
  }
  return fields
}
