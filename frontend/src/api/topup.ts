import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredQuota,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { api } from './client'
import { httpTransport } from './httpTransport'
import { ApiError } from './types'
import { getLocale } from '@/i18n'
import { serverResponseMessage } from '@/i18n/serverMessages'
import {
  TOPUP_DISCOUNT_WINDOWS,
  type TopupDiscountWindow,
} from '@/types/topupDiscount'
import { compareDecimal, isDecimal } from '@/utils/decimal'
import type { Quota } from '@/utils/quota'
export interface PayMethod {
  type: string
  name: string
  /** Method minimum in recharge units; 0 adds none to the provider minimum. */
  min_topup: number
}
export interface CreemProduct {
  productId: string
  name: string
  price: number
  currency: string
  quota: number
}
export interface TopupInfo {
  enable_online_topup: boolean
  enable_stripe_topup: boolean
  enable_creem_topup: boolean
  enable_waffo_topup: boolean
  enable_waffo_pancake_topup: boolean
  enable_gmpay_topup: boolean
  enable_redemption: boolean
  pay_methods: PayMethod[]
  waffo_pay_methods: Array<{
    name: string
    icon: string
    payMethodType: string
    payMethodName: string
  }> | null
  creem_products: string
  min_topup: number
  stripe_min_topup: number
  waffo_min_topup: number
  waffo_pancake_min_topup: number
  amount_options: number[]
  topup_link: string
}
export interface GmpayAsset {
  name: string
  token: string
  network: string
  icon?: string
  color?: string
}
export interface GmpayConfig {
  pay_address: string
  merchant_id: string
  has_secret: boolean
  assets: GmpayAsset[]
  enabled: boolean
}
export function parseGmpayConfig(value: unknown): GmpayConfig {
  const endpoint = '/api/option/gmpay/config'
  if (!isRecord(value) || !Array.isArray(value.assets))
    invalidResponse(endpoint)
  return {
    pay_address: requiredString(value.pay_address, endpoint),
    merchant_id: requiredString(value.merchant_id, endpoint),
    has_secret: requiredBoolean(value.has_secret, endpoint),
    enabled: requiredBoolean(value.enabled, endpoint),
    assets: value.assets.map((asset) => {
      if (!isRecord(asset)) invalidResponse(endpoint)
      return {
        name: requiredString(asset.name, endpoint, false),
        token: requiredString(asset.token, endpoint, false),
        network: requiredString(asset.network, endpoint, false),
        ...(asset.icon === undefined
          ? {}
          : { icon: requiredString(asset.icon, endpoint) }),
        ...(asset.color === undefined
          ? {}
          : { color: requiredString(asset.color, endpoint) }),
      }
    }),
  }
}
// PayMethods keeps min_topup as optional text: missing, blank and "0" add no
// method minimum; any other value must be a whole number, as the settings
// editor requires.
function methodMinimum(value: unknown, endpoint: string): number {
  if (value === undefined) return 0
  const text = requiredString(value, endpoint).trim()
  if (text === '') return 0
  if (!/^[0-9]+$/.test(text)) invalidResponse(endpoint)
  const minimum = Number(text)
  if (!Number.isSafeInteger(minimum)) invalidResponse(endpoint)
  return minimum
}
export function parseTopupInfo(value: unknown): TopupInfo {
  const endpoint = '/api/user/topup/info'
  if (
    !isRecord(value) ||
    !Array.isArray(value.pay_methods) ||
    !Array.isArray(value.amount_options)
  )
    invalidResponse(endpoint)
  const result = {} as TopupInfo
  for (const key of [
    'enable_online_topup',
    'enable_stripe_topup',
    'enable_creem_topup',
    'enable_waffo_topup',
    'enable_waffo_pancake_topup',
    'enable_gmpay_topup',
    'enable_redemption',
  ] as const)
    result[key] = requiredBoolean(value[key], endpoint)
  for (const key of [
    'min_topup',
    'stripe_min_topup',
    'waffo_min_topup',
    'waffo_pancake_min_topup',
  ] as const) {
    result[key] = requiredStrictInteger(value[key], endpoint)
    if (result[key] < 0) invalidResponse(endpoint)
  }
  result.pay_methods = value.pay_methods.map((raw) => {
    if (!isRecord(raw)) invalidResponse(endpoint)
    return {
      type: requiredString(raw.type, endpoint, false),
      name: requiredString(raw.name, endpoint, false),
      min_topup: methodMinimum(raw.min_topup, endpoint),
    }
  })
  result.amount_options = value.amount_options.map((n) => {
    const amount = requiredStrictInteger(n, endpoint)
    if (amount <= 0) invalidResponse(endpoint)
    return amount
  })
  result.waffo_pay_methods =
    value.waffo_pay_methods === null
      ? null
      : Array.isArray(value.waffo_pay_methods)
        ? value.waffo_pay_methods.map((raw) => {
            if (!isRecord(raw)) invalidResponse(endpoint)
            return {
              name: requiredString(raw.name, endpoint),
              icon: requiredString(raw.icon, endpoint),
              payMethodType: requiredString(raw.payMethodType, endpoint),
              payMethodName: requiredString(raw.payMethodName, endpoint),
            }
          })
        : invalidResponse(endpoint)
  result.creem_products = requiredString(value.creem_products, endpoint)
  result.topup_link = requiredString(value.topup_link, endpoint)
  return result
}

/**
 * service.TopUpQuote.PublicView: the server's price for one Epay, GMPay, Waffo
 * or Waffo Pancake top-up. Amounts and rates are exact decimal strings.
 */
export interface TopupQuote {
  expected_quota: Quota
  currency: string
  list_pay_amount: string
  pay_amount: string
  factor: string
  applied_rate: string
  amount_rate: string
  fixed_rate: string
  site_rate: string
  personal_rate: string
  cap_applied: boolean
  winner_window?: TopupDiscountWindow
}
/** Pay error code: no order exists and the response carries the new quote. */
const TOPUP_QUOTE_CHANGED = 'topup_quote_changed'
function requiredDecimal(value: unknown, endpoint: string): string {
  if (!isDecimal(value)) invalidResponse(endpoint)
  return value
}
function requiredPayAmount(value: unknown, endpoint: string): string {
  const amount = requiredDecimal(value, endpoint)
  if (compareDecimal(amount, '0') <= 0) invalidResponse(endpoint)
  return amount
}
export function parseTopupQuote(value: unknown, endpoint: string): TopupQuote {
  if (!isRecord(value)) invalidResponse(endpoint)
  const expectedQuota = requiredQuota(value.expected_quota, endpoint)
  const currency = requiredString(value.currency, endpoint)
  const window = value.winner_window
  if (
    BigInt(expectedQuota) <= 0n ||
    !/^[A-Z]{3}$/.test(currency) ||
    (window !== undefined &&
      !TOPUP_DISCOUNT_WINDOWS.includes(window as TopupDiscountWindow))
  )
    invalidResponse(endpoint)
  return {
    expected_quota: expectedQuota,
    currency,
    list_pay_amount: requiredPayAmount(value.list_pay_amount, endpoint),
    pay_amount: requiredPayAmount(value.pay_amount, endpoint),
    factor: requiredDecimal(value.factor, endpoint),
    applied_rate: requiredDecimal(value.applied_rate, endpoint),
    amount_rate: requiredDecimal(value.amount_rate, endpoint),
    fixed_rate: requiredDecimal(value.fixed_rate, endpoint),
    site_rate: requiredDecimal(value.site_rate, endpoint),
    personal_rate: requiredDecimal(value.personal_rate, endpoint),
    cap_applied: requiredBoolean(value.cap_applied, endpoint),
    ...(window === undefined
      ? {}
      : { winner_window: window as TopupDiscountWindow }),
  }
}
/** The server's new quote when a pay request was rejected for a price change. */
export function quoteChangedFrom(error: unknown): TopupQuote | null {
  if (
    !(error instanceof ApiError) ||
    error.code !== TOPUP_QUOTE_CHANGED ||
    !isRecord(error.data) ||
    !isRecord(error.data.quote)
  )
    return null
  // paymentRequest sets this data only after parseTopupQuote succeeds.
  return error.data.quote as unknown as TopupQuote
}
export function parseCreemProducts(text: string): CreemProduct[] {
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    invalidResponse('/api/user/topup/info')
  }
  if (
    !Array.isArray(value) ||
    value.some(
      (p) =>
        !isRecord(p) ||
        typeof p.productId !== 'string' ||
        !p.productId ||
        typeof p.name !== 'string' ||
        typeof p.price !== 'number' ||
        !Number.isFinite(p.price) ||
        p.price <= 0 ||
        typeof p.currency !== 'string' ||
        !Number.isSafeInteger(p.quota) ||
        Number(p.quota) <= 0
    )
  )
    invalidResponse('/api/user/topup/info')
  return value as CreemProduct[]
}
export function paymentProvider(method: string) {
  return method.startsWith('gmpay:')
    ? 'gmpay'
    : ['stripe', 'creem', 'waffo', 'waffo_pancake'].includes(method)
      ? method
      : 'epay'
}
export function enabledPayMethods(info: TopupInfo): PayMethod[] {
  const methods = (info.pay_methods ?? []).filter((method) => {
    const provider = paymentProvider(method.type)
    return provider === 'epay'
      ? info.enable_online_topup
      : info[`enable_${provider}_topup` as keyof TopupInfo] === true
  })
  if (info.enable_creem_topup && !methods.some((m) => m.type === 'creem'))
    methods.push({ type: 'creem', name: 'Creem', min_topup: 0 })
  return methods
}
export async function paymentRequest<T>(
  path: string,
  payload: unknown,
  signal?: AbortSignal,
  method: 'POST' | 'GET' = 'POST'
): Promise<{ data: T; url?: string; quote?: TopupQuote }> {
  const response: unknown = await httpTransport.request<T>(method, path, {
    data: payload,
    signal,
  })
  if (
    !isRecord(response) ||
    (response.message !== 'success' && response.message !== 'error')
  )
    invalidResponse(path)
  if (response.message === 'error' || response.success === false) {
    // GMPay puts its order fields at the top level and the Pancake pair puts
    // its failure inside data, so the two shapes cannot overlap.
    const pairFailure = parseWaffoPancakePairFailure(response.data)
    const code = typeof response.code === 'string' ? response.code : undefined
    throw new ApiError(
      serverResponseMessage(getLocale(), {
        ...response,
        message:
          typeof response.data === 'string'
            ? response.data
            : (pairFailure?.error ?? response.message),
      }) || 'Payment failed',
      {
        business: true,
        code,
        data:
          code === TOPUP_QUOTE_CHANGED
            ? { quote: parseTopupQuote(response.quote, path) }
            : (parseCreationFailure(response) ?? pairFailure),
      }
    )
  }
  if (
    !Object.hasOwn(response, 'data') ||
    (response.url !== undefined && typeof response.url !== 'string')
  )
    invalidResponse(path)
  return {
    data: response.data as T,
    ...(typeof response.url === 'string' ? { url: response.url } : {}),
    ...(response.quote === undefined
      ? {}
      : { quote: parseTopupQuote(response.quote, path) }),
  }
}

export type GmpayCreationStatus =
  'creating' | 'ready' | 'failed' | 'unknown' | 'settled'
export interface GmpayCreationFailure {
  trade_no: string
  creation_status?: GmpayCreationStatus
}
export function parseCreationFailure(
  value: unknown
): GmpayCreationFailure | undefined {
  if (
    !isRecord(value) ||
    typeof value.trade_no !== 'string' ||
    !/^GP[A-Za-z0-9]{1,64}$/.test(value.trade_no)
  )
    return undefined
  const status = value.creation_status
  if (
    status !== undefined &&
    !['creating', 'ready', 'failed', 'unknown', 'settled'].includes(
      String(status)
    )
  )
    return undefined
  return {
    trade_no: value.trade_no,
    ...(status === undefined
      ? {}
      : { creation_status: status as GmpayCreationStatus }),
  }
}

/** controller/topup_waffo_pancake.go CreateWaffoPancakePair failure data. */
export type WaffoPancakePairFailure =
  | { error: string; orphan_store?: undefined }
  | { error: string; orphan_store: true; store_id: string; store_name: string }
export function parseWaffoPancakePairFailure(
  value: unknown
): WaffoPancakePairFailure | undefined {
  if (!isRecord(value) || typeof value.error !== 'string' || !value.error)
    return undefined
  // A failure before the store exists carries only the reason.
  if (value.orphan_store === undefined) return { error: value.error }
  if (
    value.orphan_store !== true ||
    typeof value.store_id !== 'string' ||
    !value.store_id ||
    typeof value.store_name !== 'string'
  )
    return undefined
  return {
    error: value.error,
    orphan_store: true,
    store_id: value.store_id,
    store_name: value.store_name,
  }
}

export function rememberGmpayOrder(userId: number, tradeNo: string): void {
  sessionStorage.setItem(`gmpay-order:${userId}`, tradeNo)
}

export function rememberedGmpayOrder(userId: number): string {
  return sessionStorage.getItem(`gmpay-order:${userId}`) ?? ''
}
export const topupApi = {
  async info(signal?: AbortSignal) {
    return parseTopupInfo(
      await api.get<unknown>('/api/user/topup/info', undefined, { signal })
    )
  },
  /** Stripe answers a bare payable amount; the discounted providers a quote. */
  async quote(
    method: string,
    amount: number,
    signal?: AbortSignal
  ): Promise<{ pay_amount: string; quote: TopupQuote | null }> {
    const provider = paymentProvider(method)
    const prefix =
      provider === 'gmpay' || provider === 'epay'
        ? ''
        : `${provider.replaceAll('_', '-')}/`
    const path = `/api/user/${prefix}amount`
    const response = await paymentRequest<unknown>(
      path,
      { amount, payment_method: method },
      signal
    )
    if (provider === 'stripe')
      return { pay_amount: requiredPayAmount(response.data, path), quote: null }
    if (!response.quote || response.data !== response.quote.pay_amount)
      invalidResponse(path)
    return { pay_amount: response.quote.pay_amount, quote: response.quote }
  },
  pay(method: string, payload: Record<string, unknown>, signal?: AbortSignal) {
    const provider = paymentProvider(method)
    return paymentRequest<Record<string, unknown>>(
      `/api/user/${provider === 'epay' ? 'pay' : `${provider.replaceAll('_', '-')}/pay`}`,
      { ...payload, payment_method: method },
      signal
    )
  },
}
export function checkoutTarget(
  method: string,
  response: { url?: string; data: Record<string, unknown> }
): { url: string; form: Record<string, string> | null } {
  const provider = paymentProvider(method)
  const candidate =
    response.url ??
    response.data.pay_link ??
    response.data.checkout_url ??
    response.data.payment_url
  let target: URL
  try {
    if (typeof candidate !== 'string') throw new Error()
    target = new URL(candidate)
    if (
      !(provider === 'gmpay'
        ? target.protocol === 'https:'
        : ['https:', 'http:'].includes(target.protocol)) ||
      target.username ||
      target.password
    )
      throw new Error()
  } catch {
    throw new ApiError('Invalid checkout URL', { status: 502 })
  }
  const url = target.href
  let form: Record<string, string> | null = null
  if (provider === 'epay') {
    form = {}
    for (const [key, value] of Object.entries(response.data)) {
      if (typeof value !== 'string')
        throw new ApiError('Invalid checkout form', { status: 502 })
      form[key] = value
    }
  }
  return { url, form }
}
