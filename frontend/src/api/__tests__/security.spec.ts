import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/client'
import { passwordPayload, securityApi } from '@/api/security'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('account security boundary', () => {
  it('fails closed when the encryption capability is malformed', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({})
    await expect(passwordPayload('private-password')).rejects.toThrow(
      'Invalid password encryption response'
    )
    expect(get).toHaveBeenCalledWith('/api/user/login/encryption-key')
  })

  it('wraps long Unicode passwords with the version-2 RSA and AES-GCM envelope', async () => {
    const { webcrypto } = await vi.importActual<{ webcrypto: Crypto }>(
      'node:crypto'
    )
    vi.stubGlobal('crypto', webcrypto)
    const pair = await crypto.subtle.generateKey(
      {
        name: 'RSA-OAEP',
        modulusLength: 2048,
        publicExponent: new Uint8Array([1, 0, 1]),
        hash: 'SHA-256',
      },
      true,
      ['encrypt', 'decrypt']
    )
    const encoded = btoa(
      String.fromCharCode(
        ...new Uint8Array(await crypto.subtle.exportKey('spki', pair.publicKey))
      )
    )
    vi.spyOn(api, 'get').mockResolvedValue({
      enabled: true,
      kid: 'test-kid',
      public_key: `-----BEGIN PUBLIC KEY-----\n${encoded}\n-----END PUBLIC KEY-----`,
    })
    const password = '长🔐'.repeat(40)
    const payload = await passwordPayload(password)
    expect(payload.password).toBeUndefined()
    expect(payload.encryption_key_id).toBe('test-kid')
    const parts = payload.password_encrypted!.split('.')
    expect(parts).toHaveLength(4)
    expect(parts[0]).toBe('v2')
    const decode = (value: string) =>
      Uint8Array.from(atob(value), (char) => char.charCodeAt(0))
    const encoder = new TextEncoder()
    const aesRaw = await crypto.subtle.decrypt(
      { name: 'RSA-OAEP', label: encoder.encode('password-v2') },
      pair.privateKey,
      decode(parts[1]!)
    )
    const aes = await crypto.subtle.importKey('raw', aesRaw, 'AES-GCM', false, [
      'decrypt',
    ])
    const plaintext = await crypto.subtle.decrypt(
      {
        name: 'AES-GCM',
        iv: decode(parts[2]!),
        additionalData: encoder.encode('password-v2:test-kid'),
      },
      aes,
      decode(parts[3]!)
    )
    expect(new TextDecoder().decode(plaintext)).toBe(password)
    await expect(
      crypto.subtle.decrypt(
        {
          name: 'AES-GCM',
          iv: decode(parts[2]!),
          additionalData: encoder.encode('password-v2:other-kid'),
        },
        aes,
        decode(parts[3]!)
      )
    ).rejects.toThrow()
  })

  it('reads token status without writing and sends proof on revocation', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue({ exists: true, token_ref: 'fingerprint' })
    const del = vi.spyOn(api, 'delete').mockResolvedValue(null)
    const post = vi.spyOn(api, 'post')
    await securityApi.tokenStatus()
    expect(get).toHaveBeenCalledWith('/api/user/token/status')
    expect(post).not.toHaveBeenCalled()
    await securityApi.revokeToken('one-use-proof')
    expect(del).toHaveBeenCalledWith('/api/user/token', undefined, {
      headers: { 'X-Security-Proof': 'one-use-proof' },
    })
  })
})
