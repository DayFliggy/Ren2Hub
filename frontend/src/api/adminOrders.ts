import { api } from './client'
import { isDecimal } from '@/utils/decimal'
import type { AdminOrder, GmpayOrderEvidence } from '@/types/console'
import {
  invalidResponse,
  isRecord,
  requiredStrictInteger,
  requiredString,
} from './contracts'

export function parseTopUpDetail(
  value: unknown,
  orderNo: string
): {
  evidence: GmpayOrderEvidence | null
  state: Pick<AdminOrder, 'status' | 'quota' | 'paid_at'>
} {
  const native = parseTopUp(value)
  const projected = { ...native, gmpay: isRecord(value) ? value.gmpay : null }
  if (native.order_no !== orderNo)
    invalidResponse('/api/user/topup/admin/detail/:trade_no')
  const evidence =
    native.method === 'gmpay'
      ? parseGmpayOrderEvidence(projected, orderNo)
      : null
  return {
    evidence,
    state: {
      status: native.status,
      quota: native.quota,
      paid_at: native.paid_at,
    },
  }
}

export function parseGmpayOrderEvidence(
  value: unknown,
  orderNo: string
): GmpayOrderEvidence | null {
  const endpoint = '/api/user/topup/admin/detail/:trade_no'
  if (
    !isRecord(value) ||
    value.order_no !== orderNo ||
    value.method !== 'gmpay'
  ) {
    invalidResponse(endpoint)
  }
  if (value.gmpay === undefined || value.gmpay === null) return null
  if (!isRecord(value.gmpay)) invalidResponse(endpoint)
  const evidence = value.gmpay
  const status = evidence.status
  if (
    status !== 'creating' &&
    status !== 'ready' &&
    status !== 'failed' &&
    status !== 'unknown' &&
    status !== 'settled'
  ) {
    invalidResponse(endpoint)
  }
  return {
    status,
    last_failure: requiredString(evidence.last_failure, endpoint),
    gateway_trade_no: requiredString(evidence.gateway_trade_no ?? '', endpoint),
    token: requiredString(evidence.token, endpoint, false),
    network: requiredString(evidence.network, endpoint, false),
    gateway_crypto_amount: requiredString(
      evidence.gateway_crypto_amount,
      endpoint
    ),
    receive_address: requiredString(evidence.receive_address, endpoint),
    settled_crypto_amount: requiredString(
      evidence.settled_crypto_amount,
      endpoint
    ),
    settled_receive_address: requiredString(
      evidence.settled_receive_address,
      endpoint
    ),
    settled_transaction_id: requiredString(
      evidence.settled_transaction_id,
      endpoint
    ),
  }
}

export function parseTopUp(value: unknown): AdminOrder {
  const endpoint = '/api/user/topup'
  if (!isRecord(value)) invalidResponse(endpoint)
  const text = (key: string) =>
    value[key] == null ? '' : requiredString(value[key], endpoint)
  const integer = (key: string) => requiredStrictInteger(value[key], endpoint)
  if (!['pending', 'success', 'failed', 'expired'].includes(text('status')))
    invalidResponse(endpoint)
  const amount = text('pay_amount')
  if (
    amount &&
    (!isDecimal(amount) ||
      Number(amount) < 0 ||
      !Number.isFinite(Number(amount)))
  )
    invalidResponse(endpoint)
  const method = text('payment_provider') || text('payment_method')
  const known = [
    'epay',
    'gmpay',
    'stripe',
    'creem',
    'waffo',
    'waffo_pancake',
    'balance',
  ]
  return {
    id: integer('id'),
    order_no: requiredString(value.trade_no, endpoint, false),
    user_id: integer('user_id'),
    username: text('username'),
    email: text('email'),
    amount: amount ? Number(amount) : Number.NaN,
    quota: String(integer('credited_quota')),
    type: 'topup',
    currency: text('currency'),
    method: (known.includes(method) ? method : 'other') as AdminOrder['method'],
    payment_method: text('payment_method'),
    status: (text('status') === 'success'
      ? 'completed'
      : text('status')) as AdminOrder['status'],
    created: integer('create_time'),
    paid_at: integer('complete_time'),
  }
}
export async function getTopUps(
  params: Record<string, unknown>,
  signal?: AbortSignal,
  self = false
) {
  const endpoint = self ? '/api/user/topup/self' : '/api/user/topup'
  const data = await api.get<unknown>(endpoint, params, { signal })
  if (!isRecord(data) || !Array.isArray(data.items)) invalidResponse(endpoint)
  return {
    items: data.items.map(parseTopUp),
    total: requiredStrictInteger(data.total, endpoint),
    page: requiredStrictInteger(data.page, endpoint),
    page_size: requiredStrictInteger(data.page_size, endpoint),
  }
}
