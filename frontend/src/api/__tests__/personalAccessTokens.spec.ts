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
  token_ref: 'ref',
  token_hint: 'nap_****',
  last_used_ip: '',
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
      item,
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
  it('parses the legacy token from the native list, including null times', async () => {
    // controller.ListAccessTokens: {items, legacy} with *int64 times.
    const legacy = {
      token_hint: 'k9Zq',
      token_ref: 'legacy-ref',
      created_at: null,
      last_used_at: null,
      last_used_ip: '',
      retire_at: 1_900_000_000,
    }
    mocks.get.mockResolvedValueOnce({ items: [item], legacy })
    expect(await personalAccessTokensApi.list()).toEqual({
      items: [item],
      legacy,
    })
    const used = { ...legacy, created_at: 100, last_used_at: 200 }
    mocks.get.mockResolvedValueOnce({ items: [], legacy: used })
    expect((await personalAccessTokensApi.list()).legacy).toEqual(used)
    mocks.get.mockResolvedValueOnce({ items: [item], legacy: null })
    expect(await personalAccessTokensApi.list()).toEqual({
      items: [item],
      legacy: null,
    })
    for (const broken of [
      { items: [] },
      { items: [], legacy: { ...legacy, created_at: undefined } },
      { items: [], legacy: { ...legacy, last_used_at: '200' } },
      { items: [], legacy: { ...legacy, retire_at: 0 } },
      { items: [], legacy: { ...legacy, token_ref: '' } },
    ]) {
      mocks.get.mockResolvedValueOnce(broken)
      await expect(personalAccessTokensApi.list()).rejects.toMatchObject({
        code: 'INVALID_RESPONSE',
      })
    }
  })
  it('revokes the legacy token through its own route with the proof', async () => {
    mocks.delete.mockResolvedValueOnce(null)
    await personalAccessTokensApi.revokeLegacy('proof')
    expect(mocks.delete).toHaveBeenCalledWith(
      '/api/user/access_tokens/legacy',
      undefined,
      { headers: { 'X-Security-Proof': 'proof' }, signal: undefined }
    )
  })
  it('requires a valid server scope catalog', async () => {
    mocks.get.mockResolvedValue({
      groups: [
        { resources: [{ resource: 'profile', actions: [{ action: 'read' }] }] },
      ],
      max_tokens: 20,
      default_expiry_days: 30,
    })
    expect((await personalAccessTokensApi.scopes()).scopes).toEqual([
      'profile:read',
    ])
    mocks.get.mockResolvedValue({
      groups: [
        { resources: [{ resource: 'profile', actions: [{ action: 'read' }] }] },
      ],
      max_tokens: 0,
      default_expiry_days: 30,
    })
    await expect(personalAccessTokensApi.scopes()).rejects.toThrow()
  })
})
