import { api } from './client'
import { ApiError } from './types'

export interface VerificationMethod {
  method: 'password' | '2fa' | 'passkey' | 'oauth'
  available: boolean
  reason?: string
}
export interface VerificationRequirements {
  scope: string
  methods: VerificationMethod[]
  oauth_providers: Array<{ slug: string; name: string }>
  password_encryption_enabled: boolean
}
export interface SecurityOperation {
  scope: string
  context?: Record<string, unknown>
}
export interface SecurityProof {
  proof_token: string
  expires_at: number
  method: string
  scope: string
}
function base64(bytes: ArrayBuffer | Uint8Array): string {
  return btoa(String.fromCharCode(...new Uint8Array(bytes)))
}

export async function passwordPayload(
  password: string
): Promise<Record<string, string>> {
  const key = await api.get<{
    enabled: boolean
    kid?: string
    public_key?: string
  }>('/api/user/login/encryption-key')
  if (!key || typeof key.enabled !== 'boolean')
    throw new ApiError('Invalid password encryption response', { status: 502 })
  if (!key.enabled) return { password }
  if (!key.kid || !key.public_key || !crypto.subtle) {
    throw new ApiError('Password encryption is unavailable', { status: 503 })
  }
  const der = Uint8Array.from(
    atob(key.public_key.replace(/-----[^-]+-----|\s/g, '')),
    (c) => c.charCodeAt(0)
  )
  const rsa = await crypto.subtle.importKey(
    'spki',
    der,
    { name: 'RSA-OAEP', hash: 'SHA-256' },
    false,
    ['encrypt']
  )
  const aes = await crypto.subtle.generateKey(
    { name: 'AES-GCM', length: 256 },
    true,
    ['encrypt']
  )
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const encoder = new TextEncoder()
  const ciphertext = await crypto.subtle.encrypt(
    {
      name: 'AES-GCM',
      iv,
      additionalData: encoder.encode(`password-v2:${key.kid}`),
    },
    aes,
    encoder.encode(password)
  )
  const wrappedKey = await crypto.subtle.encrypt(
    { name: 'RSA-OAEP', label: encoder.encode('password-v2') },
    rsa,
    await crypto.subtle.exportKey('raw', aes)
  )
  return {
    password_encrypted: `v2.${base64(wrappedKey)}.${base64(iv)}.${base64(ciphertext)}`,
    encryption_key_id: key.kid,
  }
}

export const proofOptions = (proof: string) => ({
  headers: { 'X-Security-Proof': proof },
})

/**
 * Native TurnstileCheck reads the token only from the `turnstile` query.
 * Without a token nothing is sent, so the server reports a missing token
 * whenever Turnstile is enabled.
 */
export const turnstileParams = (token?: string): Record<string, string> =>
  token ? { turnstile: token } : {}

export const securityApi = {
  requirements: (scope: string) =>
    api.get<VerificationRequirements>('/api/verify/methods', { scope }),
  async verify(operation: SecurityOperation, method: string, value: string) {
    const credential =
      method === 'password' ? await passwordPayload(value) : { code: value }
    return api.post<SecurityProof>('/api/verify', {
      ...operation,
      method,
      ...credential,
    })
  },
}
