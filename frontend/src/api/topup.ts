import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { api } from './client'
import { httpTransport } from './httpTransport'
import { ApiError } from './types'
import { getLocale } from '@/i18n'
import { serverResponseMessage } from '@/i18n/serverMessages'
export interface PayMethod {
  type: string
  name: string
  min_topup?: string
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
  discount: Record<string, number>
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
      ...(raw.min_topup === undefined
        ? {}
        : { min_topup: requiredString(raw.min_topup, endpoint) }),
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
  if (
    !isRecord(value.discount) ||
    Object.values(value.discount).some(
      (v) => typeof v !== 'number' || !Number.isFinite(v) || v <= 0
    )
  )
    invalidResponse(endpoint)
  result.discount = value.discount as Record<string, number>
  return result
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
    methods.push({ type: 'creem', name: 'Creem' })
  return methods
}
export async function paymentRequest<T>(
  path: string,
  payload: unknown,
  signal?: AbortSignal,
  method: 'POST' | 'GET' = 'POST'
): Promise<{ data: T; url?: string }> {
  const response: unknown = await httpTransport.request<T>(method, path, {
    data: payload,
    signal,
  })
  if (
    !isRecord(response) ||
    (response.message !== 'success' && response.message !== 'error')
  )
    invalidResponse(path)
  if (response.message === 'error' || response.success === false)
    throw new ApiError(
      serverResponseMessage(getLocale(), {
        ...response,
        message:
          typeof response.data === 'string' ? response.data : response.message,
      }) || 'Payment failed',
      { business: true, data: parseCreationFailure(response) }
    )
  if (
    !Object.hasOwn(response, 'data') ||
    (response.url !== undefined && typeof response.url !== 'string')
  )
    invalidResponse(path)
  return {
    data: response.data as T,
    ...(typeof response.url === 'string' ? { url: response.url } : {}),
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
  async quote(method: string, amount: number, signal?: AbortSignal) {
    const provider = paymentProvider(method)
    const prefix =
      provider === 'gmpay' || provider === 'epay'
        ? ''
        : `${provider.replaceAll('_', '-')}/`
    return (
      await paymentRequest<string>(
        `/api/user/${prefix}amount`,
        { amount, payment_method: method },
        signal
      )
    ).data
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
