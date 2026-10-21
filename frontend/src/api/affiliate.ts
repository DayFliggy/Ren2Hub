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
import type { Quota } from '@/utils/quota'

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

export const affiliateApi = {
  async info(signal?: AbortSignal): Promise<AffiliateInfo> {
    const endpoint = '/api/user/affiliate'
    const data = record(
      await api.get(endpoint, undefined, { signal }),
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
