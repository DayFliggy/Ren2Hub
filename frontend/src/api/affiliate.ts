import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parsePage,
  requiredBoolean,
  requiredQuota,
  requiredStrictInteger,
  requiredStrictNumber,
  requiredString,
} from './contracts'
import { viewerTzOffset } from '@/utils/format'
import type { Quota } from '@/utils/quota'
import type { InviteRecord } from '@/types/console'

export type AffiliateRecord = InviteRecord

export interface AffiliateMonth {
  month: string
  new_count: number
  cumulative: number
  reward: Quota
}
export interface AffiliateInfo {
  code: string
  code_enabled: boolean
  invited: number
  effective_rate_bps: number
  effective_rate_percent: number
  transferable: Quota
  transfer_enabled: boolean
  outstanding_offset: Quota
  frozen_reward: Quota
  reward_total: Quota
  monthly_series: AffiliateMonth[]
  policy: { enabled: boolean; freeze_hours: number }
}
export interface AffiliateInvitee {
  masked_name: string
  invited_at: number
  reward_quota: Quota
}
export interface AffiliateUserPolicy {
  user_id: number
  code: string
  code_custom: boolean
  enabled: boolean
  rate_bps: number | null
  effective_rate_bps: number
  inviter_id: number
  invited_at: number
}
export interface AffiliatePolicyUpdate {
  rate_bps: number | null
  code?: string
  enabled?: boolean
  reset_code?: boolean
}

function record(value: unknown, endpoint: string) {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}
function count(value: unknown, endpoint: string) {
  const parsed = requiredStrictInteger(value, endpoint)
  if (parsed < 0) invalidResponse(endpoint)
  return parsed
}
function quota(value: unknown, endpoint: string) {
  const parsed = requiredQuota(value, endpoint)
  if (BigInt(parsed) < 0n) invalidResponse(endpoint)
  return parsed
}
function parsePolicy(value: unknown, endpoint: string): AffiliateUserPolicy {
  const data = record(value, endpoint)
  return {
    user_id: count(data.user_id, endpoint),
    code: requiredString(data.code, endpoint),
    code_custom: requiredBoolean(data.code_custom, endpoint),
    enabled: requiredBoolean(data.enabled, endpoint),
    rate_bps: data.rate_bps === null ? null : count(data.rate_bps, endpoint),
    effective_rate_bps: count(data.effective_rate_bps, endpoint),
    inviter_id: count(data.inviter_id, endpoint),
    invited_at: count(data.invited_at, endpoint),
  }
}

function parseAffiliateRecord(
  value: unknown,
  endpoint: string
): AffiliateRecord {
  const row = record(value, endpoint)
  if (
    row.kind !== 'accrue' &&
    row.kind !== 'transfer' &&
    row.kind !== 'reversal' &&
    row.kind !== 'deduction'
  )
    invalidResponse(endpoint)
  return {
    id: count(row.id, endpoint),
    user_id: count(row.user_id, endpoint),
    invitee_id: count(row.invitee_id, endpoint),
    kind: row.kind,
    skip_reason: requiredString(row.skip_reason, endpoint),
    source_type: requiredString(row.source_type, endpoint),
    source_id: requiredString(row.source_id, endpoint),
    base_quota: quota(row.base_quota, endpoint),
    rate_bps: count(row.rate_bps, endpoint),
    reward_quota: quota(row.reward_quota, endpoint),
    credited_quota: quota(row.credited_quota ?? 0, endpoint),
    debt_offset_quota: quota(row.debt_offset_quota ?? 0, endpoint),
    debt_added_quota: quota(row.debt_added_quota ?? 0, endpoint),
    frozen_until: count(row.frozen_until, endpoint),
    thawed_at: count(row.thawed_at, endpoint),
    created_at: count(row.created_at, endpoint),
    available_after: quota(row.available_after, endpoint),
    frozen_after: quota(row.frozen_after, endpoint),
    wallet_after: requiredQuota(row.wallet_after, endpoint),
  }
}

export const affiliateApi = {
  async info(signal?: AbortSignal): Promise<AffiliateInfo> {
    const endpoint = '/api/user/affiliate'
    // monthly_series months follow the viewer's calendar.
    const data = record(
      await api.get(endpoint, { tz_offset: viewerTzOffset() }, { signal }),
      endpoint
    )
    const policy = record(data.policy, endpoint)
    if (!Array.isArray(data.monthly_series)) invalidResponse(endpoint)
    return {
      code: requiredString(data.code, endpoint),
      code_enabled: requiredBoolean(data.code_enabled, endpoint),
      invited: count(data.invited, endpoint),
      effective_rate_bps: count(data.effective_rate_bps, endpoint),
      effective_rate_percent: requiredStrictNumber(
        data.effective_rate_percent,
        endpoint
      ),
      transferable: quota(data.transferable, endpoint),
      transfer_enabled:
        data.transfer_enabled === undefined
          ? true
          : requiredBoolean(data.transfer_enabled, endpoint),
      outstanding_offset: quota(data.outstanding_offset ?? 0, endpoint),
      frozen_reward: quota(data.frozen_reward, endpoint),
      reward_total: quota(data.reward_total, endpoint),
      policy: {
        enabled: requiredBoolean(policy.enabled, endpoint),
        freeze_hours: count(policy.freeze_hours, endpoint),
      },
      monthly_series: data.monthly_series.map((value) => {
        const month = record(value, endpoint)
        return {
          month: requiredString(month.month, endpoint),
          new_count: count(month.new_count, endpoint),
          cumulative: count(month.cumulative, endpoint),
          reward: quota(month.reward, endpoint),
        }
      }),
    }
  },
  async records(page: number, pageSize: number, signal?: AbortSignal) {
    const endpoint = '/api/user/affiliate/records'
    return parsePage(
      await api.get(endpoint, { p: page, page_size: pageSize }, { signal }),
      endpoint,
      parseAffiliateRecord
    )
  },
  async invitees(
    page: number,
    pageSize: number,
    order: 'asc' | 'desc',
    signal?: AbortSignal
  ) {
    const endpoint = '/api/user/affiliate/invitees'
    return parsePage(
      await api.get(
        endpoint,
        { p: page, page_size: pageSize, order },
        { signal }
      ),
      endpoint,
      (value, endpoint): AffiliateInvitee => {
        const row = record(value, endpoint)
        return {
          masked_name: requiredString(row.masked_name, endpoint),
          invited_at: count(row.invited_at, endpoint),
          reward_quota: quota(row.reward_quota, endpoint),
        }
      }
    )
  },
  async transfer(operationID: string) {
    const endpoint = '/api/user/affiliate/transfer'
    const data = record(
      await api.post(endpoint, { operation_id: operationID }),
      endpoint
    )
    return {
      replayed: requiredBoolean(data.replayed, endpoint),
      transferred_quota: quota(data.transferred_quota, endpoint),
      wallet_quota: requiredQuota(data.wallet_quota, endpoint),
    }
  },
  async userPolicy(id: number, signal?: AbortSignal) {
    const endpoint = `/api/admin/affiliate/users/${id}`
    return parsePolicy(await api.get(endpoint, undefined, { signal }), endpoint)
  },
  async updateUserPolicy(id: number, input: AffiliatePolicyUpdate) {
    const endpoint = `/api/admin/affiliate/users/${id}`
    return parsePolicy(await api.put(endpoint, input), endpoint)
  },
}
