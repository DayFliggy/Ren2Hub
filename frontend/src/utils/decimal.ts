/** Exact decimal prices; no conversion through binary floating point. */
export function isDecimal(value: unknown): value is string {
  return (
    typeof value === 'string' &&
    /^(?:0|[1-9]\d*)(?:\.\d+)?$/.test(value) &&
    value.length <= 128
  )
}
function parts(value: string): { coefficient: bigint; scale: number } {
  if (!isDecimal(value)) throw new RangeError('Invalid decimal')
  const [whole = '0', fraction = ''] = value.split('.')
  return { coefficient: BigInt(whole + fraction), scale: fraction.length }
}
function render(coefficient: bigint, scale: number): string {
  if (!scale) return coefficient.toString()
  const digits = coefficient.toString().padStart(scale + 1, '0')
  return (
    `${digits.slice(0, -scale)}.${digits.slice(-scale)}`.replace(
      /\.?0+$/,
      ''
    ) || '0'
  )
}
export function multiplyDecimal(left: string, right: string): string {
  const a = parts(left),
    b = parts(right)
  return render(a.coefficient * b.coefficient, a.scale + b.scale)
}
/** Local-currency input is converted explicitly, with 18-place half-up USD. */
export function divideDecimal(left: string, right: string): string {
  const a = parts(left),
    b = parts(right)
  if (b.coefficient === 0n) throw new RangeError('Zero exchange rate')
  const numerator = a.coefficient * 10n ** BigInt(b.scale + 18)
  const denominator = b.coefficient * 10n ** BigInt(a.scale)
  return render((numerator + denominator / 2n) / denominator, 18)
}

/** Divide while retaining significant digits for prices smaller than 1e-18. */
export function divideDecimalSignificant(
  left: string,
  right: string,
  significantDigits = 18
): string {
  const a = parts(left)
  const b = parts(right)
  if (b.coefficient === 0n) throw new RangeError('Zero exchange rate')
  if (a.coefficient === 0n) return '0'
  if (
    !Number.isInteger(significantDigits) ||
    significantDigits < 1 ||
    significantDigits > 128
  ) {
    throw new RangeError('Invalid significant digit count')
  }

  const exponentShift = b.scale - a.scale
  let exponent =
    a.coefficient.toString().length -
    b.coefficient.toString().length +
    exponentShift
  const compareAtPower = (power: number): number => {
    const shift = exponentShift - power
    if (shift >= 0) {
      const scaledLeft = a.coefficient * 10n ** BigInt(shift)
      return scaledLeft === b.coefficient
        ? 0
        : scaledLeft > b.coefficient
          ? 1
          : -1
    }
    const scaledRight = b.coefficient * 10n ** BigInt(-shift)
    return a.coefficient === scaledRight
      ? 0
      : a.coefficient > scaledRight
        ? 1
        : -1
  }
  if (compareAtPower(exponent) < 0) exponent--

  const scale = significantDigits - 1 - exponent
  if (scale >= 0) {
    const numerator = a.coefficient * 10n ** BigInt(b.scale + scale)
    const denominator = b.coefficient * 10n ** BigInt(a.scale)
    return render((numerator + denominator / 2n) / denominator, scale)
  }
  const roundingScale = -scale
  const numerator = a.coefficient * 10n ** BigInt(b.scale)
  const denominator = b.coefficient * 10n ** BigInt(a.scale + roundingScale)
  const rounded = (numerator + denominator / 2n) / denominator
  return render(rounded * 10n ** BigInt(roundingScale), 0)
}
export function shiftDecimal(value: string, places: number): string {
  const parsed = parts(value),
    scale = parsed.scale + places
  return scale < 0
    ? render(parsed.coefficient * 10n ** BigInt(-scale), 0)
    : render(parsed.coefficient, scale)
}
export function addDecimal(left: string, right: string): string {
  const a = parts(left),
    b = parts(right),
    scale = Math.max(a.scale, b.scale)
  return render(
    a.coefficient * 10n ** BigInt(scale - a.scale) +
      b.coefficient * 10n ** BigInt(scale - b.scale),
    scale
  )
}
export function compareDecimal(left: string, right: string): number {
  const a = parts(left),
    b = parts(right),
    scale = Math.max(a.scale, b.scale)
  const difference =
    a.coefficient * 10n ** BigInt(scale - a.scale) -
    b.coefficient * 10n ** BigInt(scale - b.scale)
  return difference > 0n ? 1 : difference < 0n ? -1 : 0
}
export function formatDecimalUSD(value: string, digits = 3): string {
  digits = Math.min(digits, 3)
  const parsed = parts(value),
    divisor = 10n ** BigInt(Math.max(0, parsed.scale - digits))
  const rounded =
    parsed.scale > digits
      ? (parsed.coefficient + divisor / 2n) / divisor
      : parsed.coefficient * 10n ** BigInt(digits - parsed.scale)
  const text = rounded.toString().padStart(digits + 1, '0')
  const whole = (digits ? text.slice(0, -digits) : text).replace(
    /\B(?=(\d{3})+(?!\d))/g,
    ','
  )
  return `$${whole}${digits ? `.${text.slice(-digits)}` : ''}`
}

export function formatUnitPriceUSD(value: string): string {
  return compareDecimal(value, '0') > 0 && compareDecimal(value, '0.001') < 0
    ? '<$0.001'
    : formatDecimalUSD(value)
}

/** Read-only decimal display; raw input and billing snapshots remain unchanged. */
export function formatDecimal(value: string): string {
  const negative = value.startsWith('-')
  const result = formatDecimalUSD(negative ? value.slice(1) : value)
    .slice(1)
    .replaceAll(',', '')
  return (negative ? '-' : '') + result
}
