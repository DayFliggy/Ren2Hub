import { api } from '@/api/client'
import { ClientError } from '@/api/clientError'
import type { LocalizedTextValue } from '@/features/task-plugins/types'
export const PLUGIN_LOGIN_BROADCAST_CHANNEL = 'ren2hub-plugin-login'
export type PluginCredentialSummary = {
  label?: string
  account_id?: string
  expires_at?: number
  needs_login?: boolean
}
export type PluginLoginField = {
  name: string
  label?: LocalizedTextValue | null
  title?: LocalizedTextValue | null
  description?: LocalizedTextValue | null
  type?: 'text' | 'password' | 'textarea'
  placeholder?: LocalizedTextValue | null
  required?: boolean
  secret?: boolean
}
export type PluginLoginStep =
  | { type: 'redirect'; url: string }
  | { type: 'paste'; url: string; hint?: LocalizedTextValue }
  | {
      type: 'device'
      user_code: string
      verification_uri: string
      verification_uri_complete?: string
      interval?: number
      expires_at?: number
    }
  | { type: 'pending'; interval?: number }
  | { type: 'fields'; fields: PluginLoginField[] }
  | { type: 'done'; credential?: PluginCredentialSummary }
export type PluginLoginInput =
  { callback_url: string } | { fields: Record<string, string> } | { poll: true }
export function safeLoginUrl(value: string): string | null {
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) &&
      !url.username &&
      !url.password
      ? url.href
      : null
  } catch {
    return null
  }
}
export function parsePluginLoginStep(value: unknown): PluginLoginStep {
  if (!value || typeof value !== 'object')
    throw new ClientError('pluginLoginInvalidResponse')
  const step = value as Record<string, unknown>
  switch (step.type) {
    case 'redirect':
    case 'paste':
      if (typeof step.url === 'string' && safeLoginUrl(step.url))
        return step as PluginLoginStep
      break
    case 'device':
      if (
        typeof step.user_code === 'string' &&
        typeof step.verification_uri === 'string' &&
        safeLoginUrl(step.verification_uri) &&
        (step.verification_uri_complete === undefined ||
          (typeof step.verification_uri_complete === 'string' &&
            safeLoginUrl(step.verification_uri_complete)))
      )
        return step as PluginLoginStep
      break
    case 'pending':
    case 'done':
      return step as PluginLoginStep
    case 'fields':
      if (
        Array.isArray(step.fields) &&
        step.fields.every(
          (f) => f && typeof f === 'object' && typeof f.name === 'string'
        )
      )
        return step as PluginLoginStep
      break
  }
  throw new ClientError('pluginLoginInvalidResponse')
}
/** A login replaces the channel's one credential, so it names no key line. */
export async function startPluginLogin(
  channelId: number,
  signal?: AbortSignal
) {
  const data = await api.post<{ flow: string; step: unknown }>(
    `/api/channel/${channelId}/plugin_login`,
    {},
    { signal }
  )
  if (typeof data.flow !== 'string' || !data.flow)
    throw new ClientError('pluginLoginInvalidFlow')
  return { flow: data.flow, step: parsePluginLoginStep(data.step) }
}
export async function continuePluginLogin(
  channelId: number,
  flow: string,
  input: PluginLoginInput,
  signal?: AbortSignal
) {
  const data = await api.post<{ step: unknown }>(
    `/api/channel/${channelId}/plugin_login/continue`,
    { flow, input },
    { signal }
  )
  return parsePluginLoginStep(data.step)
}
export async function submitPluginLoginCallback(
  plugin: string,
  params: Record<string, string>,
  signal?: AbortSignal
) {
  const data = await api.post<{ channel_id: number; step: unknown }>(
    '/api/channel/plugin_login/callback',
    { plugin, params },
    { signal }
  )
  if (!Number.isSafeInteger(data.channel_id) || data.channel_id <= 0)
    throw new ClientError('pluginLoginInvalidChannel')
  return { channel_id: data.channel_id, step: parsePluginLoginStep(data.step) }
}
export async function getPluginCredentials(
  channelId: number,
  signal?: AbortSignal
) {
  const data = await api.get<{ items: PluginCredentialSummary[] | null }>(
    `/api/channel/${channelId}/plugin_credentials`,
    undefined,
    { signal }
  )
  return data.items ?? []
}
export function readPluginLoginMessage(
  data: unknown,
  channelId: number,
  flow: string
): PluginLoginStep | null {
  if (!data || typeof data !== 'object') return null
  const message = data as Record<string, unknown>
  if (message.channel_id !== channelId || message.state !== flow) return null
  try {
    return parsePluginLoginStep(message.step)
  } catch {
    return null
  }
}
