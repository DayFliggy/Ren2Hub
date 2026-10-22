import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parseStringArray,
  requiredBoolean,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import { proofOptions } from './security'

export type PasskeyCreationOptions = NonNullable<
  NonNullable<Parameters<typeof navigator.credentials.create>[0]>['publicKey']
>
type WireCredentialDescriptor = Omit<
  NonNullable<PasskeyCreationOptions['excludeCredentials']>[number],
  'id'
> & { id: string }
type WirePasskeyCreationOptions = Omit<
  PasskeyCreationOptions,
  'challenge' | 'user' | 'excludeCredentials'
> & {
  challenge: string
  user: Omit<PasskeyCreationOptions['user'], 'id'> & { id: string }
  excludeCredentials?: WireCredentialDescriptor[]
}

function record(value: unknown, endpoint: string): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}

function emailFlow(value: unknown) {
  const endpoint = '/api/oauth/email/bind'
  const row = record(value, endpoint)
  return {
    flow_token: requiredString(row.flow_token, endpoint, false),
    old_email_required: requiredBoolean(row.old_email_required, endpoint),
    resend_at: requiredStrictInteger(row.resend_at, endpoint),
  }
}

export const accountSecurityApi = {
  async passkeyStatus() {
    const endpoint = '/api/user/passkey'
    const row = record(await api.get(endpoint), endpoint)
    const lastUsed =
      row.last_used_at == null
        ? null
        : requiredString(row.last_used_at, endpoint, false)
    if (lastUsed !== null && !Number.isFinite(Date.parse(lastUsed)))
      invalidResponse(endpoint)
    return {
      enabled: requiredBoolean(row.enabled, endpoint),
      last_used_at: lastUsed,
    }
  },
  async twoFactorStatus() {
    const endpoint = '/api/user/2fa/status'
    const row = record(await api.get(endpoint), endpoint)
    const remaining =
      row.backup_codes_remaining === undefined
        ? null
        : requiredStrictInteger(row.backup_codes_remaining, endpoint)
    if (remaining !== null && remaining < 0) invalidResponse(endpoint)
    return {
      enabled: requiredBoolean(row.enabled, endpoint),
      backup_codes_remaining: remaining,
    }
  },
  async oauthBindings() {
    const endpoint = '/api/user/oauth/bindings'
    const data = await api.get(endpoint)
    if (!Array.isArray(data)) invalidResponse(endpoint)
    return data.map((value) => {
      const row = record(value, endpoint)
      const id = requiredStrictInteger(row.provider_id, endpoint)
      if (id < 1) invalidResponse(endpoint)
      return {
        provider_id: id,
        provider_name: requiredString(row.provider_name, endpoint),
        provider_slug: requiredString(row.provider_slug, endpoint, false),
        provider_icon:
          row.provider_icon === undefined
            ? undefined
            : requiredString(row.provider_icon, endpoint),
        provider_user_id: requiredString(row.provider_user_id, endpoint),
      }
    })
  },
  async startEmailBinding(email: string, proof: string) {
    return emailFlow(
      await api.post(
        '/api/oauth/email/bind/start',
        { email },
        proofOptions(proof)
      )
    )
  },
  async resendEmailBinding(flowToken: string) {
    return emailFlow(
      await api.post('/api/oauth/email/bind/resend', { flow_token: flowToken })
    )
  },
  async beginPasskeyRegistration(proof: string) {
    const endpoint = '/api/user/passkey/register/begin'
    const data = record(
      await api.post(endpoint, undefined, proofOptions(proof)),
      endpoint
    )
    const options = record(data.options, endpoint)
    const key = record(options.publicKey, endpoint)
    requiredString(key.challenge, endpoint, false)
    requiredString(record(key.user, endpoint).id, endpoint, false)
    return {
      flow_token: requiredString(data.flow_token, endpoint, false),
      options: { publicKey: key as WirePasskeyCreationOptions },
    }
  },
  finishPasskeyRegistration(
    flowToken: string,
    credential: Record<string, unknown>,
    proof: string
  ) {
    return api.post<unknown>(
      '/api/user/passkey/register/finish',
      { flow_token: flowToken, credential },
      proofOptions(proof)
    )
  },
  removePasskey: (proof: string) =>
    api.delete<unknown>('/api/user/passkey', undefined, proofOptions(proof)),
  async setupTwoFactor(proof: string) {
    const endpoint = '/api/user/2fa/setup'
    const row = record(
      await api.post(endpoint, undefined, proofOptions(proof)),
      endpoint
    )
    // Mirrors service.TwoFASetup; qr_code_data is an otpauth:// URI, not an image.
    return {
      secret: requiredString(row.secret, endpoint, false),
      qr_code_data: requiredString(row.qr_code_data, endpoint, false),
      backup_codes: parseStringArray(row.backup_codes, endpoint),
      flow_token: requiredString(row.flow_token, endpoint, false),
      expires_at: requiredStrictInteger(row.expires_at, endpoint),
    }
  },
  enableTwoFactor: (flowToken: string, code: string) =>
    api.post<unknown>('/api/user/2fa/enable', { flow_token: flowToken, code }),
  async changeTwoFactor(action: 'regenerate' | 'disable', proof: string) {
    const endpoint =
      action === 'regenerate'
        ? '/api/user/2fa/backup_codes'
        : '/api/user/2fa/disable'
    const row = record(
      await api.post(endpoint, undefined, proofOptions(proof)),
      endpoint
    )
    if (action === 'regenerate')
      return {
        ...row,
        action: 'regenerate' as const,
        backup_codes: parseStringArray(row.backup_codes, endpoint),
      }
    return { ...row, action: 'disable' as const }
  },
  bindEmail: (flowToken: string, newCode: string, oldCode: string) =>
    api.post('/api/oauth/email/bind', {
      flow_token: flowToken,
      new_code: newCode,
      old_code: oldCode,
    }),
  bindWechat: (code: string, proof: string) =>
    api.post('/api/oauth/wechat/bind', { code }, proofOptions(proof)),
  unbindOauth: (id: number, proof: string) =>
    api.delete(
      `/api/user/oauth/bindings/${id}`,
      undefined,
      proofOptions(proof)
    ),
}
