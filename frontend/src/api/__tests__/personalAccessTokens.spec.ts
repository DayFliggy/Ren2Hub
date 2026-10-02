import { beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('../client', () => ({ api: mocks }))
import {
  parsePersonalAccessToken,
  personalAccessTokensApi,
} from '../personalAccessTokens'
const item = {
  id: 1,
  name: 'ci',
  scopes: ['profile:read'],
  created_at: 1,
  last_used_at: 0,
  expires_at: 4000000000,
  revoked_at: 0,
}
beforeEach(() => vi.clearAllMocks())
describe('PAT response contract', () => {
  it('projects metadata without retaining credential fields', () => {
    expect(
      parsePersonalAccessToken({ ...item, digest: 'private', token: 'private' })
    ).toEqual(item)
    expect(() => parsePersonalAccessToken({ ...item, scopes: null })).toThrow()
    expect(() => parsePersonalAccessToken({ ...item, id: '1' })).toThrow()
  })
  it('rejects a malformed one-time secret and preserves proof headers', async () => {
    mocks.post.mockResolvedValue({
      token: 'short',
      personal_access_token: item,
    })
    await expect(
      personalAccessTokensApi.create(
        { name: 'ci', scopes: item.scopes, expires_at: 0 },
        'proof'
      )
    ).rejects.toThrow()
    expect(mocks.post.mock.calls[0]?.[2]).toMatchObject({
      headers: { 'X-Security-Proof': 'proof' },
    })
  })
  it('requires a valid server scope catalog', async () => {
    mocks.get.mockResolvedValue({
      scopes: ['profile:read'],
      limit: 20,
      default_lifetime_days: 30,
    })
    expect((await personalAccessTokensApi.scopes()).scopes).toEqual([
      'profile:read',
    ])
    mocks.get.mockResolvedValue({
      scopes: ['profile:read'],
      limit: 0,
      default_lifetime_days: 30,
    })
    await expect(personalAccessTokensApi.scopes()).rejects.toThrow()
  })
})
