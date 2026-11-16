import type { AdminUser } from '@/types/console'
import {
  invalidResponse,
  isRecord,
  requiredQuota,
  requiredStrictInteger,
  requiredString,
} from './contracts'

export function parseAdminUserPage(value: unknown): {
  items: AdminUser[]
  total: number
} {
  const endpoint = '/api/user/'
  if (!isRecord(value) || !Array.isArray(value.items)) invalidResponse(endpoint)
  const total = requiredStrictInteger(value.total, endpoint)
  if (total < 0) invalidResponse(endpoint)
  return {
    total,
    items: value.items.map((value) => {
      if (!isRecord(value)) invalidResponse(endpoint)
      const result: Record<string, unknown> = {}
      for (const key of [
        'id',
        'role',
        'status',
        'quota',
        'used_quota',
        'request_count',
        'max_concurrency',
        'aff_count',
        'aff_quota',
        'aff_history_quota',
        'aff_frozen_quota',
        'inviter_id',
        'created_at',
        'last_login_at',
      ]) {
        result[key] = requiredStrictInteger(value[key], endpoint)
        if (key !== 'quota' && Number(result[key]) < 0)
          invalidResponse(endpoint)
      }
      if (
        Number(result.id) < 1 ||
        ![1, 10, 100].includes(Number(result.role)) ||
        ![1, 2].includes(Number(result.status))
      )
        invalidResponse(endpoint)
      for (const key of ['username', 'display_name', 'email'])
        result[key] = requiredString(value[key], endpoint)
      result.remark =
        value.remark === undefined ? '' : requiredString(value.remark, endpoint)
      // gorm.DeletedAt always serializes: null, or the RFC 3339 deletion time.
      if (
        value.DeletedAt !== null &&
        (typeof value.DeletedAt !== 'string' ||
          Number.isNaN(Date.parse(value.DeletedAt)))
      )
        invalidResponse(endpoint)
      result.deleted = value.DeletedAt !== null
      if (value.admin_permissions !== undefined) {
        if (
          !isRecord(value.admin_permissions) ||
          Object.values(value.admin_permissions).some(
            (actions) =>
              !isRecord(actions) ||
              Object.values(actions).some(
                (allowed) => typeof allowed !== 'boolean'
              )
          )
        )
          invalidResponse(endpoint)
        result.admin_permissions = value.admin_permissions
      }
      return result as AdminUser
    }),
  }
}

/**
 * The bonus of one administrator quota change (controller/user_quota.go);
 * only additions earn one, and a replay repeats the committed bonus.
 */
export function parseQuotaAdjustmentBonus(value: unknown): string {
  const endpoint = '/api/user/manage'
  if (!isRecord(value)) invalidResponse(endpoint)
  const bonus = requiredQuota(value.bonus_quota, endpoint)
  if (BigInt(bonus) < 0n) invalidResponse(endpoint)
  return bonus
}
