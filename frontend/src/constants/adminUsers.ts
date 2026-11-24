import {
  quotaMeter,
  quotaMeterColor,
  type QuotaMeter,
} from '@/utils/quotaMeter'
import type { AdminUser, AdminUserRole, AdminUserSortBy } from '@/types/console'

/** Backend role ladder (common/constants.go). Ascending privilege. */
export const ADMIN_USER_ROLES: readonly AdminUserRole[] = [0, 1, 10, 100]

/** Roles that can be assigned from the form. Guests are registration-only. */
export const ADMIN_USER_ASSIGNABLE_ROLES: readonly AdminUserRole[] = [
  1, 10, 100,
]

/** Upstream `validate:"max=20"` on User.Username, which counts runes. */
export const ADMIN_USERNAME_MAX_LENGTH = 20

/** Go unicode.IsSpace: these Latin-1 characters plus \p{Z}. Every member is a
 * single UTF-16 code unit. Unlike String.prototype.trim, U+FEFF is not one of
 * them and U+0085 is. */
const GO_SPACE = /[\t\n\v\f\r \u0085\u00A0\p{Z}]/u

/** strings.TrimSpace, which upstream applies to usernames before saving. */
export function trimGoSpace(value: string): string {
  let start = 0
  let end = value.length
  while (start < end && GO_SPACE.test(value.charAt(start))) start++
  while (end > start && GO_SPACE.test(value.charAt(end - 1))) end--
  return value.slice(start, end)
}

/**
 * Mirrors upstream CreateUser/UpdateUser: TrimSpace, reject empty, then
 * `max=20`. The backend has no character-set rule, so none is imposed here.
 */
export function isValidAdminUsername(value: string): boolean {
  const username = trimGoSpace(value)
  return (
    username.length > 0 && [...username].length <= ADMIN_USERNAME_MAX_LENGTH
  )
}

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

type AdminUserStatusView = Pick<AdminUser, 'status' | 'deleted'>

/** A soft-deleted row keeps its last status, but deletion is what it shows. */
export function adminUserStatusTone(
  user: AdminUserStatusView
): 'success' | 'danger' {
  return user.status === 1 && !user.deleted ? 'success' : 'danger'
}

export interface AdminUserQuotaMeter extends QuotaMeter {
  color: string
}

/** The users table follows the shared soft-balance policy in quotaMeter. */
export function adminUserQuotaMeter(
  user: Pick<AdminUser, 'quota' | 'used_quota'>
): AdminUserQuotaMeter {
  const meter = quotaMeter(user.quota, user.used_quota)
  return { ...meter, color: quotaMeterColor(meter.state) }
}

export function adminUserStatusLabelKey(
  user: AdminUserStatusView
): 'users.statusEnabled' | 'users.statusDisabled' | 'users.statusDeleted' {
  if (user.deleted) return 'users.statusDeleted'
  return user.status === 1 ? 'users.statusEnabled' : 'users.statusDisabled'
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
 * Soft-deleted rows are read-only, as in upstream web: edits load the target
 * through the scoped `GetUserById` and fail, and although `ManageUser` finds
 * them unscoped, a status change on a deleted account means nothing.
 */
export function canManageAdminUser(
  target: Pick<AdminUser, 'id' | 'role' | 'deleted'>,
  operator: AdminOperator | null,
  action: AdminUserAction = 'edit'
): boolean {
  if (!operator || target.deleted || target.id === operator.id) return false
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
