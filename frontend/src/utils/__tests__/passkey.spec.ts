import { afterEach, describe, expect, it, vi } from 'vitest'

import { assertPasskey } from '@/utils/passkey'

afterEach(() => vi.unstubAllGlobals())

describe('passkey assertion', () => {
  it('reports a prompt without a credential as a translatable NotAllowedError', async () => {
    vi.stubGlobal('navigator', {
      credentials: { get: vi.fn().mockResolvedValue(null) },
    })
    await expect(
      assertPasskey({ publicKey: { challenge: 'Y2hhbGxlbmdl' } })
    ).rejects.toMatchObject({
      name: 'NotAllowedError',
      code: 'passkeyCancelled',
    })
  })
})
