import type { AdminOrder, GmpayOrderEvidence } from '@/types/console'
import {
  invalidResponse,
  isRecord,
  requiredQuota,
  requiredStrictInteger,
  requiredString,
} from './contracts'

export function parseGmpayOrderDetail(
  value: unknown,
  orderNo: string
): {
  evidence: GmpayOrderEvidence | null
  state: Pick<AdminOrder, 'status' | 'quota' | 'paid_at'>
} {
  const evidence = parseGmpayOrderEvidence(value, orderNo)
  const endpoint = '/api/next/admin/orders/:order_no'
  if (!isRecord(value)) invalidResponse(endpoint)
  const status = value.status
  if (status !== 'pending' && status !== 'completed' && status !== 'failed')
    invalidResponse(endpoint)
  const quota = requiredQuota(value.quota, endpoint)
  const paidAt = requiredStrictInteger(value.paid_at, endpoint)
  if (BigInt(quota) < 0n || paidAt < 0) invalidResponse(endpoint)
  return { evidence, state: { status, quota, paid_at: paidAt } }
}

export function parseGmpayOrderEvidence(
  value: unknown,
  orderNo: string
): GmpayOrderEvidence | null {
  const endpoint = '/api/next/admin/orders/:order_no'
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
