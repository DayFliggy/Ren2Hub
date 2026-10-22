import { afterEach, expect, it, vi } from 'vitest'
import { api } from '../client'
import { adminUserBindingsApi } from '../adminUserBindings'
afterEach(() => vi.restoreAllMocks())
it('rejects malformed custom provider identifiers before creating action URLs', async () => {
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({})
    .mockResolvedValueOnce([{ provider_id: '../other', provider_name: 'Bad' }])
  await expect(adminUserBindingsApi.list(42)).rejects.toThrow(
    'Invalid API response'
  )
})
it('uses the administrator endpoints and never returns identity values', async () => {
  vi.spyOn(api, 'get')
    .mockResolvedValueOnce({
      github_id: 'private-identity',
      email: '',
      telegram_id: '',
    })
    .mockResolvedValueOnce([
      {
        provider_id: 9,
        provider_name: 'Company',
        provider_user_id: 'private-custom',
      },
    ])
  const bindings = await adminUserBindingsApi.list(42)
  expect(bindings).toEqual([
    {
      key: 'github',
      name: 'GitHub',
      path: '/api/user/42/bindings/github',
      proofContext: { user_id: 42, binding_type: 'github' },
    },
    {
      key: 'oauth-9',
      name: 'Company',
      path: '/api/user/42/oauth/bindings/9',
      proofContext: { user_id: 42, provider_id: 9 },
    },
  ])
  const remove = vi.spyOn(api, 'delete').mockResolvedValue(undefined)
  await adminUserBindingsApi.remove(bindings[0]!, 'builtin-proof')
  await adminUserBindingsApi.remove(bindings[1]!, 'custom-proof')
  expect(remove.mock.calls).toEqual([
    [
      '/api/user/42/bindings/github',
      undefined,
      { headers: { 'X-Security-Proof': 'builtin-proof' } },
    ],
    [
      '/api/user/42/oauth/bindings/9',
      undefined,
      { headers: { 'X-Security-Proof': 'custom-proof' } },
    ],
  ])
})
