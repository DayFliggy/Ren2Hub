import { describe, expect, it } from 'vitest'

import { parseAdminUserPage } from '../adminUserContracts'

/** Shape of one upstream `GET /api/user/` item (model.User JSON). */
function rawUser(overrides: Record<string, unknown> = {}) {
  return {
    id: 2,
    username: 'member',
    display_name: 'Member',
    email: '',
    role: 1,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    max_concurrency: 0,
    aff_count: 0,
    aff_quota: 0,
    aff_history_quota: 0,
    aff_frozen_quota: 0,
    inviter_id: 0,
    created_at: 1,
    last_login_at: 0,
    DeletedAt: null,
    ...overrides,
  }
}

function parseOne(overrides: Record<string, unknown>) {
  return parseAdminUserPage({ items: [rawUser(overrides)], total: 1 }).items[0]!
}

function withoutDeletedAt() {
  const { DeletedAt: _omitted, ...rest } = rawUser()
  return rest
}

describe('admin user page contract', () => {
  it('restores an empty remark that upstream omits with omitempty', () => {
    expect(parseOne({}).remark).toBe('')
    expect(parseOne({ remark: 'VIP contact' }).remark).toBe('VIP contact')
  })

  it('rejects a remark that is not a string', () => {
    expect(() => parseOne({ remark: null })).toThrow()
    expect(() => parseOne({ remark: 1 })).toThrow()
  })

  it('reads soft deletion from the unscoped list DeletedAt field', () => {
    expect(parseOne({}).deleted).toBe(false)
    expect(
      parseOne({ DeletedAt: '2026-10-20T08:00:00.123+08:00' }).deleted
    ).toBe(true)
  })

  it('rejects a missing or malformed DeletedAt', () => {
    expect(() =>
      parseAdminUserPage({ items: [withoutDeletedAt()], total: 1 })
    ).toThrow()
    for (const DeletedAt of [0, 1, '', 'yesterday', true])
      expect(() => parseOne({ DeletedAt })).toThrow()
  })
})
