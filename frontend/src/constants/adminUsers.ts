import { quotaInteger, quotaRatio } from '@/utils/quota'
import type {
  AdminUser,
  AdminUserRole,
  AdminUserSortBy,
  AdminUserStatus,
} from '@/types/console'

/** Backend role ladder (common/constants.go). Ascending privilege. */
export const ADMIN_USER_ROLES: readonly AdminUserRole[] = [0, 1, 10, 100]

/** Roles that can be assigned from the form. Guests are registration-only. */
export const ADMIN_USER_ASSIGNABLE_ROLES: readonly AdminUserRole[] = [
  1, 10, 100,
]

export const ADMIN_USER_OPTIONAL_FIELDS = [
  'id',
  'status',
  'role',
  'quota',
  'invite',
  'lastLogin',
  'createdTime',
] as const

export type AdminUserOptionalField = (typeof ADMIN_USER_OPTIONAL_FIELDS)[number]

/** ID and registration time stay off by default to keep the table narrow. */
export const ADMIN_USER_DEFAULT_VISIBLE_FIELDS: AdminUserOptionalField[] =
  ADMIN_USER_OPTIONAL_FIELDS.filter(
    (field) => field !== 'id' && field !== 'createdTime'
  )

// v2: the group column joined the defaults, so a new key lets it appear on
// browsers that already stored a field selection.
export const ADMIN_USER_VISIBLE_FIELDS_STORAGE_KEY =
  'ren2hub_admin_user_visible_fields_v2'

export function sanitizeAdminUserVisibleFields(
  fields: readonly string[]
): AdminUserOptionalField[] {
  return ADMIN_USER_OPTIONAL_FIELDS.filter((field) => fields.includes(field))
}

export const ADMIN_USER_SORT_FIELDS: AdminUserSortBy[] = [
  'id',
  'username',
  'quota',
  'used_quota',
  'created_time',
  'last_login_time',
]

export function adminUserStatusTone(
  status: AdminUserStatus
): 'success' | 'danger' {
  return status === 1 ? 'success' : 'danger'
}

export type AdminUserQuotaState = 'normal' | 'low' | 'exhausted'

export interface AdminUserQuotaMeter {
  state: AdminUserQuotaState
  /** Consumed share of the lifetime allowance, 0-100. */
  percent: number
  /** Remaining share, 0-100 — what the "only N% left" note reports. */
  remainingPercent: number
  color: string
}

/** Below this remaining share the row is worth an operator's attention. */
const QUOTA_LOW_REMAINING_RATIO = 0.1

/**
 * Deliberately NOT CapacityMeter's policy. Channel capacity is a hard ceiling —
 * at 80% it is nearly unroutable, so it warns early. A user's quota is a soft
 * balance that a top-up refills, so 80% consumed means nothing; only "almost
 * out" and "out" are worth signalling. Same reason the users table stopped
 * rendering a used/total pair: the total is not a real domain value.
 */
export function adminUserQuotaMeter(
  user: Pick<AdminUser, 'quota' | 'used_quota'>
): AdminUserQuotaMeter {
  // The display denominator can exceed one account field's int64 range.
  const lifetime =
    quotaInteger(String(user.quota)) + quotaInteger(String(user.used_quota))
  const percent =
    lifetime > 0n
      ? Math.min(
          100,
          Math.max(0, quotaRatio(String(user.used_quota), lifetime) * 100)
        )
      : 0
  const remainingRatio =
    lifetime > 0n ? quotaRatio(String(user.quota), lifetime) : 0

  // quota <= 0 covers both a spent-out account and one never funded; either way
  // the operator's takeaway is identical — this user cannot spend.
  const state: AdminUserQuotaState =
    BigInt(user.quota) <= 0n
      ? 'exhausted'
      : remainingRatio < QUOTA_LOW_REMAINING_RATIO
        ? 'low'
        : 'normal'

  return {
    state,
    percent,
    remainingPercent: Math.round(remainingRatio * 100),
    color:
      state === 'exhausted'
        ? 'var(--status-danger)'
        : state === 'low'
          ? 'var(--status-warning)'
          : 'var(--signal)',
  }
}

export function adminUserStatusLabelKey(
  status: AdminUserStatus
): 'users.statusEnabled' | 'users.statusDisabled' {
  return status === 1 ? 'users.statusEnabled' : 'users.statusDisabled'
}

/**
 * Single source of truth for the role→tone ladder, shared by the admin table
 * and the profile page. Rust red marks root because red already reads as the
 * "administration" signal in this console, not as an error.
 */
export function adminUserRoleTone(
  role: number
): 'danger' | 'warning' | 'neutral' {
  if (role >= 100) return 'danger'
  if (role >= 10) return 'warning'
  return 'neutral'
}

export function adminUserRoleLabelKey(
  role: number
): 'users.roleRoot' | 'users.roleAdmin' | 'users.roleUser' | 'users.roleGuest' {
  if (role >= 100) return 'users.roleRoot'
  if (role >= 10) return 'users.roleAdmin'
  if (role >= 1) return 'users.roleUser'
  return 'users.roleGuest'
}

export interface AdminOperator {
  id: number
  level: 10 | 100
}

/**
 * The signed-in administrator as seen by the user-management guards. The level
 * comes from the auth store's role flags; `null` means no admin capability at
 * all. The backend remains the authorization boundary.
 */
export function adminOperator(auth: {
  user: { id: number } | null
  isRoot: boolean
  isAdmin: boolean
}): AdminOperator | null {
  if (!auth.user || !auth.isAdmin) return null
  return { id: auth.user.id, level: auth.isRoot ? 100 : 10 }
}

/**
 * `edit` covers profile updates, quota, referral policy, binding clears and
 * role changes. Enable shares its rule; disable and delete carry extra
 * backend branches.
 */
export type AdminUserAction = 'edit' | 'enable' | 'disable' | 'delete'

/** The manage action behind a row's status toggle. */
export function adminUserStatusAction(
  user: Pick<AdminUser, 'status'>
): 'enable' | 'disable' {
  return user.status === 1 ? 'disable' : 'enable'
}

/**
 * UI-side guard only — never an authorization boundary. Mirrors the backend
 * branch for each action:
 * - edit and enable: canManageTargetRole (controller/user.go:377), so root may edit root;
 * - disable: ManageUser also refuses a root target (controller/user.go:999);
 * - delete: DeleteUser requires a strictly higher role (controller/user.go:814),
 *   so not even root can delete root.
 * Your own row is always excluded; self-service edits belong on the profile.
 */
export function canManageAdminUser(
  target: Pick<AdminUser, 'id' | 'role'>,
  operator: AdminOperator | null,
  action: AdminUserAction = 'edit'
): boolean {
  if (!operator || target.id === operator.id) return false
  if (action === 'delete') return operator.level > target.role
  if (action === 'disable' && target.role === 100) return false
  return operator.level === 100 || operator.level > target.role
}

/**
 * Roles the form may submit. Creating needs a strictly lower role
 * (controller/user.go:894). Editing keeps the current role or uses one manage
 * step: root promotes a common user to admin, and demote turns an admin into a
 * common user. A root account has no role change.
 */
export function adminUserRoleChoices(
  operator: AdminOperator | null,
  current?: AdminUserRole
): AdminUserRole[] {
  if (!operator) return []
  if (current === undefined)
    return ADMIN_USER_ASSIGNABLE_ROLES.filter((role) => role < operator.level)
  if (current === 100) return [100]
  if (operator.level === 100) return [1, 10]
  return [current]
}
