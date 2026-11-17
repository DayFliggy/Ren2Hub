import { BRAND_MARKS, type BrandMark } from './brandMarks'

export type PaymentIcon =
  | { kind: 'image'; src: string }
  | { kind: 'brand'; mark: BrandMark }
  | { kind: 'letter'; letter: string; color: string }
  | { kind: 'generic'; name: 'card' | 'bank' | 'link' }

type BrandKey = keyof typeof BRAND_MARKS

// Identifiers administrators and gateways use for the same brand or chain.
const ALIASES: Record<string, BrandKey> = {
  wxpay: 'wechat',
  usdt: 'tether',
  btc: 'bitcoin',
  eth: 'ethereum',
  erc20: 'ethereum',
  bsc: 'bnbchain',
  bnb: 'bnbchain',
  bep20: 'bnbchain',
  binance: 'bnbchain',
  smartchain: 'bnbchain',
  'bnb-smart-chain': 'bnbchain',
  matic: 'polygon',
  pol: 'polygon',
  sol: 'solana',
}
// Chains without a reliable mark in the icon set.
const LETTERS: Record<string, { letter: string; color: string }> = {
  tron: { letter: 'T', color: '#EB0029' },
  trx: { letter: 'T', color: '#EB0029' },
  trc20: { letter: 'T', color: '#EB0029' },
  aptos: { letter: 'A', color: 'currentColor' },
  apt: { letter: 'A', color: 'currentColor' },
  plasma: { letter: 'P', color: '#16A34A' },
  usdc: { letter: 'U', color: '#2775CA' },
}

function brandFor(id: string): PaymentIcon | null {
  const key = id.trim().toLowerCase()
  if (!key) return null
  // Own keys only, so names such as "constructor" match nothing.
  const brand = Object.hasOwn(BRAND_MARKS, key)
    ? (key as BrandKey)
    : Object.hasOwn(ALIASES, key)
      ? ALIASES[key]
      : undefined
  if (brand) return { kind: 'brand', mark: BRAND_MARKS[brand] }
  return Object.hasOwn(LETTERS, key)
    ? { kind: 'letter', ...LETTERS[key]! }
    : null
}

/** Only plain https images; userinfo or other schemes are not rendered. */
function imageFor(value: string): PaymentIcon | null {
  if (!/^https:\/\//i.test(value.trim())) return null
  try {
    const url = new URL(value.trim())
    if (url.protocol !== 'https:' || url.username || url.password) return null
    return { kind: 'image', src: url.href }
  } catch {
    return null
  }
}

/** GMPay methods are `gmpay:<token>.<network>`; the network tells them apart. */
function methodIcon(type: string): PaymentIcon | null {
  if (type.startsWith('gmpay:')) {
    const dot = type.indexOf('.')
    if (dot < 0) return null
    const network = type.slice(dot + 1)
    return (
      brandFor(network) ?? {
        kind: 'letter',
        letter: network.charAt(0).toUpperCase() || '?',
        color: 'currentColor',
      }
    )
  }
  if (type === 'creem') return { kind: 'generic', name: 'bank' }
  if (type === 'waffo' || type === 'waffo_pancake')
    return { kind: 'generic', name: 'card' }
  return brandFor(type)
}

/**
 * The icon for a payment method or backup link. A configured https image or
 * built-in identifier wins; an unknown identifier falls back to the type.
 */
export function paymentIcon(options: {
  icon?: string
  type?: string
  fallback: 'card' | 'link'
}): PaymentIcon {
  const configured = options.icon ?? ''
  return (
    imageFor(configured) ??
    brandFor(configured) ??
    (options.type ? methodIcon(options.type) : null) ?? {
      kind: 'generic',
      name: options.fallback,
    }
  )
}
