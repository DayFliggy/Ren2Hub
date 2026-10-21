export type SettingsNotifyType = 'email' | 'webhook' | 'bark' | 'gotify'

export type SettingsBindingId =
  | 'email'
  | 'github'
  | 'linuxdo'
  | 'discord'
  | 'oidc'
  | 'wechat'
  | 'telegram'
  | `custom:${number}`

export interface SettingsBinding {
  id: SettingsBindingId
  bound: boolean
  account: string
  providerId?: number
  providerSlug?: string
  providerName?: string
  providerIcon?: string
}

export interface NotificationSettings {
  notifyType: SettingsNotifyType
  // Whole quota as text; empty means the site default (stored as 0).
  quotaWarningThreshold: string
  // undefined keeps the backend default: reminders on, channel notices for
  // root only. Unset switches are left out of the save request.
  quotaWarningEnabled?: boolean
  channelNotifyEnabled?: boolean
  // Not editable here, but the backend overwrites it on every save.
  acceptUnsetModelRatio: boolean
  notificationEmail: string
  webhookUrl: string
  webhookSecret: string
  barkUrl: string
  gotifyUrl: string
  gotifyToken: string
  gotifyPriority: number
  walletReminder: boolean
  upstreamModelUpdateNotify: boolean
  recordIpLog: boolean
}
