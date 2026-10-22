import { describe, expect, it } from 'vitest'

import {
  ADMIN_USER_ASSIGNABLE_ROLES,
  ADMIN_USER_DEFAULT_VISIBLE_FIELDS,
  ADMIN_USER_OPTIONAL_FIELDS,
  ADMIN_USER_ROLES,
  adminOperator,
  adminUserQuotaMeter,
  adminUserRoleChoices,
  adminUserRoleLabelKey,
  adminUserRoleTone,
  adminUserStatusAction,
  adminUserStatusLabelKey,
  adminUserStatusTone,
  canManageAdminUser,
  sanitizeAdminUserVisibleFields,
} from '@/constants/adminUsers'

describe('admin user role ladder', () => {
  it('matches the backend ladder in ascending privilege order', () => {
    // common/constants.go: guest 0 < common 1 < admin 10 < root 100
    expect(ADMIN_USER_ROLES).toEqual([0, 1, 10, 100])
    expect([...ADMIN_USER_ROLES].sort((a, b) => a - b)).toEqual([
      ...ADMIN_USER_ROLES,
    ])
  })

  it('never offers guest as an assignable role', () => {
    expect(ADMIN_USER_ASSIGNABLE_ROLES).not.toContain(0)
    expect(ADMIN_USER_ASSIGNABLE_ROLES.every((role) => role > 0)).toBe(true)
  })

  it('maps roles onto the same tone ladder the profile page uses', () => {
    expect(adminUserRoleTone(100)).toBe('danger')
    expect(adminUserRoleTone(10)).toBe('warning')
    expect(adminUserRoleTone(1)).toBe('neutral')
    expect(adminUserRoleTone(0)).toBe('neutral')
  })

  it('labels every rung, including guest', () => {
    expect(adminUserRoleLabelKey(100)).toBe('users.roleRoot')
    expect(adminUserRoleLabelKey(10)).toBe('users.roleAdmin')
    expect(adminUserRoleLabelKey(1)).toBe('users.roleUser')
    expect(adminUserRoleLabelKey(0)).toBe('users.roleGuest')
  })

  it('treats an unknown higher role as at least root-tier', () => {
    expect(adminUserRoleTone(255)).toBe('danger')
    expect(adminUserRoleLabelKey(255)).toBe('users.roleRoot')
  })
})

describe('admin user status', () => {
  it('pairs each status with a tone and a label', () => {
    expect(adminUserStatusTone(1)).toBe('success')
    expect(adminUserStatusTone(2)).toBe('danger')
    expect(adminUserStatusLabelKey(1)).toBe('users.statusEnabled')
    expect(adminUserStatusLabelKey(2)).toBe('users.statusDisabled')
  })
})

describe('adminUserQuotaMeter', () => {
  it('renders valid balances when the display denominator exceeds the safe number range when summed', () => {
    expect(
      adminUserQuotaMeter({
        quota: 9007199254740991,
        used_quota: 9007199254740991,
      })
    ).toMatchObject({ state: 'normal', percent: 50, remainingPercent: 50 })
  })

  it('reports the consumed share and stays quiet while the balance is healthy', () => {
    expect(adminUserQuotaMeter({ quota: 800, used_quota: 200 })).toEqual({
      state: 'normal',
      percent: 20,
      remainingPercent: 80,
      color: 'var(--signal)',
    })
  })

  it('flips to low strictly below a tenth remaining, not at it', () => {
    // Exactly 10% left is still normal; a hair under is not.
    expect(adminUserQuotaMeter({ quota: 100, used_quota: 900 }).state).toBe(
      'normal'
    )
    expect(adminUserQuotaMeter({ quota: 99, used_quota: 901 })).toMatchObject({
      state: 'low',
      remainingPercent: 10,
      color: 'var(--status-warning)',
    })
  })

  it('treats a spent-out and a never-funded account the same way', () => {
    expect(adminUserQuotaMeter({ quota: 0, used_quota: 1000 })).toMatchObject({
      state: 'exhausted',
      percent: 100,
      color: 'var(--status-danger)',
    })
    // Never funded: nothing consumed, but still unable to spend.
    expect(adminUserQuotaMeter({ quota: 0, used_quota: 0 })).toMatchObject({
      state: 'exhausted',
      percent: 0,
      remainingPercent: 0,
    })
  })

  it('clamps a negative balance rather than rendering a reversed ring', () => {
    const meter = adminUserQuotaMeter({
      quota: -50,
      used_quota: 100,
    })
    expect(meter.state).toBe('exhausted')
    expect(meter.percent).toBeGreaterThanOrEqual(0)
    expect(meter.percent).toBeLessThanOrEqual(100)
  })
})

describe('admin user field visibility', () => {
  it('hides ID and registration time by default to keep the table narrow', () => {
    expect(ADMIN_USER_DEFAULT_VISIBLE_FIELDS).not.toContain('id')
    expect(ADMIN_USER_DEFAULT_VISIBLE_FIELDS).not.toContain('createdTime')
    expect(ADMIN_USER_DEFAULT_VISIBLE_FIELDS).toContain('quota')
  })

  it('drops unknown fields and restores the canonical column order', () => {
    expect(sanitizeAdminUserVisibleFields(['quota', 'id', 'nope'])).toEqual([
      'id',
      'quota',
    ])
    expect(sanitizeAdminUserVisibleFields([])).toEqual([])
    expect(sanitizeAdminUserVisibleFields(['__proto__', 'toString'])).toEqual(
      []
    )
  })

  it('round-trips the full field set unchanged', () => {
    expect(
      sanitizeAdminUserVisibleFields([...ADMIN_USER_OPTIONAL_FIELDS])
    ).toEqual([...ADMIN_USER_OPTIONAL_FIELDS])
  })
})

describe('adminOperator', () => {
  const user = { id: 7 }

  it('derives the level from the auth role flags', () => {
    expect(adminOperator({ user, isRoot: true, isAdmin: true })).toEqual({
      id: 7,
      level: 100,
    })
    expect(adminOperator({ user, isRoot: false, isAdmin: true })).toEqual({
      id: 7,
      level: 10,
    })
  })

  it('has no operator without a session or admin role', () => {
    expect(adminOperator({ user, isRoot: false, isAdmin: false })).toBeNull()
    expect(
      adminOperator({ user: null, isRoot: true, isAdmin: true })
    ).toBeNull()
  })
})

describe('canManageAdminUser', () => {
  const admin = { id: 1, level: 10 } as const
  const root = { id: 1, level: 100 } as const
  const actions = ['edit', 'enable', 'disable', 'delete'] as const

  it('refuses without an operator and on the operator’s own row', () => {
    for (const action of actions) {
      expect(canManageAdminUser({ id: 9, role: 1 }, null, action)).toBe(false)
      expect(canManageAdminUser({ id: 1, role: 1 }, admin, action)).toBe(false)
      expect(canManageAdminUser({ id: 1, role: 10 }, root, action)).toBe(false)
    }
  })

  it('lets an admin act only on strictly lower roles', () => {
    for (const action of actions) {
      expect(canManageAdminUser({ id: 9, role: 1 }, admin, action)).toBe(true)
      expect(canManageAdminUser({ id: 9, role: 10 }, admin, action)).toBe(false)
      expect(canManageAdminUser({ id: 9, role: 100 }, admin, action)).toBe(
        false
      )
    }
  })

  it('lets root edit and enable another root but never disable or delete it', () => {
    const otherRoot = { id: 9, role: 100 } as const
    expect(canManageAdminUser(otherRoot, root)).toBe(true)
    expect(canManageAdminUser(otherRoot, root, 'enable')).toBe(true)
    expect(canManageAdminUser(otherRoot, root, 'disable')).toBe(false)
    expect(canManageAdminUser(otherRoot, root, 'delete')).toBe(false)
    for (const action of actions)
      expect(canManageAdminUser({ id: 9, role: 10 }, root, action)).toBe(true)
  })

  it('maps the status toggle to the native manage action', () => {
    expect(adminUserStatusAction({ status: 1 })).toBe('disable')
    expect(adminUserStatusAction({ status: 2 })).toBe('enable')
  })
})

describe('adminUserRoleChoices', () => {
  const admin = { id: 1, level: 10 } as const
  const root = { id: 1, level: 100 } as const

  it('creates only roles strictly below the operator', () => {
    expect(adminUserRoleChoices(admin)).toEqual([1])
    expect(adminUserRoleChoices(root)).toEqual([1, 10])
    expect(adminUserRoleChoices(null)).toEqual([])
  })

  it('offers only the role changes ManageUser accepts', () => {
    expect(adminUserRoleChoices(root, 1)).toEqual([1, 10])
    expect(adminUserRoleChoices(root, 10)).toEqual([1, 10])
    expect(adminUserRoleChoices(root, 100)).toEqual([100])
    expect(adminUserRoleChoices(admin, 1)).toEqual([1])
  })
})
