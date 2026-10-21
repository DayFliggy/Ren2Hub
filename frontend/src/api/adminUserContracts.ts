import type { AdminUser } from '@/types/console'
import {
  invalidResponse,
  isRecord,
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
