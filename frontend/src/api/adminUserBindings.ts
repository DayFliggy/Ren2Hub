import { api } from './client'
import { proofOptions } from './security'
import { invalidResponse, isRecord } from './contracts'

const builtins = [
  ['email', 'email', 'Email'],
  ['github', 'github_id', 'GitHub'],
  ['discord', 'discord_id', 'Discord'],
  ['oidc', 'oidc_id', 'OIDC'],
  ['wechat', 'wechat_id', 'WeChat'],
  ['telegram', 'telegram_id', 'Telegram'],
  ['linuxdo', 'linux_do_id', 'Linux DO'],
] as const

/** admin.user.binding.clear context: exactly one of binding_type or provider_id. */
export type AdminUserBindingProofContext =
  | { user_id: number; binding_type: string }
  | { user_id: number; provider_id: number }

export interface AdminUserBinding {
  key: string
  name: string
  path: string
  /** Built next to `path` so the proof always names the binding being cleared. */
  proofContext: AdminUserBindingProofContext
}

export const adminUserBindingsApi = {
  async list(id: number, signal?: AbortSignal): Promise<AdminUserBinding[]> {
    const base = `/api/user/${id}`
    const [user, custom] = await Promise.all([
      api.get<unknown>(base, undefined, { signal }),
      api.get<unknown>(`${base}/oauth/bindings`, undefined, { signal }),
    ])
    if (!isRecord(user) || !Array.isArray(custom)) invalidResponse(base)
    const providers = custom.map((item) => {
      if (
        !isRecord(item) ||
        !Number.isSafeInteger(item.provider_id) ||
        Number(item.provider_id) <= 0 ||
        typeof item.provider_name !== 'string'
      )
        invalidResponse(base)
      return {
        provider_id: item.provider_id as number,
        provider_name: item.provider_name,
      }
    })
    return [
      ...builtins
        .filter(
          ([, field]) => typeof user[field] === 'string' && user[field] !== ''
        )
        .map(([key, , name]) => ({
          key,
          name,
          path: `${base}/bindings/${key}`,
          proofContext: { user_id: id, binding_type: key },
        })),
      ...providers.map((item) => ({
        key: `oauth-${item.provider_id}`,
        name: item.provider_name,
        path: `${base}/oauth/bindings/${item.provider_id}`,
        proofContext: { user_id: id, provider_id: item.provider_id },
      })),
    ]
  },
  remove: (binding: AdminUserBinding, proof: string) =>
    api.delete(binding.path, undefined, proofOptions(proof)),
}
