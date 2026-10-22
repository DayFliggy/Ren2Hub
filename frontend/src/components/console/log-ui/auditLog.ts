import type { AuditLogCategory, AuditLogItem } from '@/types/console'

type Translate = (key: string, params?: Record<string, unknown>) => string

/**
 * Stable action identifiers recorded by the server, mapped to localized
 * templates. Retired identifiers stay listed because historical rows keep the
 * name they were written with.
 */
export const AUDIT_LOG_ACTION_LABEL_KEYS: Record<string, string> = {
  login: 'auditLogs.actions.login',
  'access_token.generate': 'auditLogs.actions.accessTokenGenerate',
  'access_token.revoke': 'auditLogs.actions.accessTokenRevoke',
  'token.create': 'auditLogs.actions.tokenCreate',
  'token.update': 'auditLogs.actions.tokenUpdate',
  'token.status_update': 'auditLogs.actions.tokenStatusUpdate',
  'token.delete': 'auditLogs.actions.tokenDelete',
  'token.delete_batch': 'auditLogs.actions.tokenDeleteBatch',
  'token.key_view': 'auditLogs.actions.tokenKeyView',
  'token.key_view_batch': 'auditLogs.actions.tokenKeyViewBatch',
  'user.create': 'auditLogs.actions.userCreate',
  'user.update': 'auditLogs.actions.userUpdate',
  'user.delete': 'auditLogs.actions.userDelete',
  'user.delete_batch': 'auditLogs.actions.userDeleteBatch',
  'user.manage': 'auditLogs.actions.userManage',
  'user.quota_add': 'auditLogs.actions.userQuotaAdd',
  'user.quota_subtract': 'auditLogs.actions.userQuotaSubtract',
  'user.quota_override': 'auditLogs.actions.userQuotaOverride',
  'user.quota_adjust': 'auditLogs.actions.userQuotaAdjust',
  'user.binding_start': 'auditLogs.actions.userBindingStart',
  'user.binding_bind': 'auditLogs.actions.userBindingBind',
  'user.binding_unbind': 'auditLogs.actions.userBindingUnbind',
  'user.binding_clear': 'auditLogs.actions.userBindingClear',
  'user.email_binding_resend': 'auditLogs.actions.userEmailBindingResend',
  'user.2fa_disable': 'auditLogs.actions.userTwoFactorForceDisable',
  'user.2fa_setup': 'auditLogs.actions.userTwoFactorSetup',
  'user.2fa_setup_start': 'auditLogs.actions.userTwoFactorSetup',
  'user.2fa_enable': 'auditLogs.actions.userTwoFactorEnable',
  'user.2fa_disable_self': 'auditLogs.actions.userTwoFactorSelfDisable',
  'user.2fa_self_disable': 'auditLogs.actions.userTwoFactorSelfDisable',
  'user.2fa_backup_codes': 'auditLogs.actions.userTwoFactorBackupCodes',
  'user.2fa_backup_codes_regenerate':
    'auditLogs.actions.userTwoFactorBackupCodes',
  'user.passkey_register': 'auditLogs.actions.userPasskeyRegister',
  'user.passkey_delete': 'auditLogs.actions.userPasskeyDelete',
  'user.topup_complete': 'auditLogs.actions.userTopupComplete',
  'user.reset_passkey': 'auditLogs.actions.userResetPasskey',
  'user.oauth_unbind': 'auditLogs.actions.userOauthUnbind',
  'user.status_update': 'auditLogs.actions.userStatusUpdate',
  'user.status_update_batch': 'auditLogs.actions.userStatusUpdateBatch',
  'user.checkin': 'auditLogs.actions.userCheckin',
  'user.security_verify': 'auditLogs.actions.userSecurityVerify',
  'user.password_change': 'auditLogs.actions.userPasswordChange',
  'user.account_delete': 'auditLogs.actions.userAccountDelete',
  'user.registration_bonus': 'auditLogs.actions.userRegistrationBonus',
  'user.invitee_bonus': 'auditLogs.actions.userInviteeBonus',
  'user.inviter_bonus': 'auditLogs.actions.userInviterBonus',
  'affiliate.code.reset': 'auditLogs.actions.affiliateCodeReset',
  'affiliate.user.update': 'auditLogs.actions.affiliateUserUpdate',
  'gateway.config.update': 'auditLogs.actions.gatewayConfigUpdate',
  'gateway.user.update': 'auditLogs.actions.gatewayUserUpdate',
  'option.update': 'auditLogs.actions.optionUpdate',
  'option.bulk_update': 'auditLogs.actions.optionBulkUpdate',
  'option.payment_compliance': 'auditLogs.actions.optionPaymentCompliance',
  'option.reset_ratio': 'auditLogs.actions.optionResetRatio',
  'option.clear_affinity_cache': 'auditLogs.actions.optionClearAffinityCache',
  'option.passkey_domains': 'auditLogs.actions.optionPasskeyDomains',
  'option.passkey_domains_confirmed':
    'auditLogs.actions.optionPasskeyDomainsConfirmed',
  'option.passkey_domains_blocked':
    'auditLogs.actions.optionPasskeyDomainsBlocked',
  'option.passkey_domains_failed':
    'auditLogs.actions.optionPasskeyDomainsFailed',
  'custom_oauth.create': 'auditLogs.actions.customOauthCreate',
  'custom_oauth.update': 'auditLogs.actions.customOauthUpdate',
  'custom_oauth.delete': 'auditLogs.actions.customOauthDelete',
  'performance.clear_disk_cache': 'auditLogs.actions.performanceClearDiskCache',
  'performance.gc': 'auditLogs.actions.performanceGc',
  'performance.clear_logs': 'auditLogs.actions.performanceClearLogs',
  'channel.create': 'auditLogs.actions.channelCreate',
  'channel.update': 'auditLogs.actions.channelUpdate',
  'channel.delete': 'auditLogs.actions.channelDelete',
  'channel.delete_batch': 'auditLogs.actions.channelDeleteBatch',
  'channel.delete_disabled': 'auditLogs.actions.channelDeleteDisabled',
  'channel.key_view': 'auditLogs.actions.channelKeyView',
  'channel.status_update': 'auditLogs.actions.channelStatusUpdate',
  'channel.status_update_batch': 'auditLogs.actions.channelStatusUpdateBatch',
  'channel.tag_disable': 'auditLogs.actions.channelTagDisable',
  'channel.tag_enable': 'auditLogs.actions.channelTagEnable',
  'channel.tag_edit': 'auditLogs.actions.channelTagEdit',
  'channel.tag_batch_set': 'auditLogs.actions.channelTagBatchSet',
  'channel.copy': 'auditLogs.actions.channelCopy',
  'channel.multi_key_manage': 'auditLogs.actions.channelMultiKeyManage',
  'channel.upstream_apply': 'auditLogs.actions.channelUpstreamApply',
  'channel.upstream_apply_all': 'auditLogs.actions.channelUpstreamApplyAll',
  'channel.upstream_detect_all': 'auditLogs.actions.channelUpstreamDetectAll',
  'channel.lab_balance_sync': 'auditLogs.actions.channelLabBalanceSync',
  'channel.lab_model_test': 'auditLogs.actions.channelLabModelTest',
  'channel.lab_delete': 'auditLogs.actions.channelLabDelete',
  'redemption.create': 'auditLogs.actions.redemptionCreate',
  'redemption.update': 'auditLogs.actions.redemptionUpdate',
  'redemption.delete': 'auditLogs.actions.redemptionDelete',
  'redemption.delete_invalid': 'auditLogs.actions.redemptionDeleteInvalid',
  'redemption.delete_batch': 'auditLogs.actions.redemptionDeleteBatch',
  'redemption.status_update': 'auditLogs.actions.redemptionStatusUpdate',
  'prefill_group.create': 'auditLogs.actions.prefillGroupCreate',
  'prefill_group.update': 'auditLogs.actions.prefillGroupUpdate',
  'prefill_group.delete': 'auditLogs.actions.prefillGroupDelete',
  'vendor.create': 'auditLogs.actions.vendorCreate',
  'vendor.update': 'auditLogs.actions.vendorUpdate',
  'vendor.delete': 'auditLogs.actions.vendorDelete',
  'model.create': 'auditLogs.actions.modelCreate',
  'model.update': 'auditLogs.actions.modelUpdate',
  'model.delete': 'auditLogs.actions.modelDelete',
  'model.sync_upstream': 'auditLogs.actions.modelSyncUpstream',
  'ticket.reply': 'auditLogs.actions.ticketReply',
  'ticket.status_update': 'auditLogs.actions.ticketStatusUpdate',
  'ticket.assign': 'auditLogs.actions.ticketAssign',
  'log.cleanup_start': 'auditLogs.actions.logCleanupStart',
  generic: 'auditLogs.actions.generic',
}

export const AUDIT_LOG_PARAM_LABEL_KEYS: Record<string, string> = {
  id: 'auditLogs.params.id',
  ids: 'auditLogs.params.ids',
  target_user_id: 'auditLogs.params.targetUserId',
  username: 'auditLogs.params.username',
  role: 'auditLogs.params.role',
  action: 'auditLogs.params.action',
  quota: 'auditLogs.params.quota',
  from: 'auditLogs.params.from',
  to: 'auditLogs.params.to',
  delta: 'auditLogs.params.delta',
  value: 'auditLogs.params.value',
  mode: 'auditLogs.params.mode',
  status: 'auditLogs.params.status',
  count: 'auditLogs.params.count',
  name: 'auditLogs.params.name',
  type: 'auditLogs.params.type',
  tag: 'auditLogs.params.tag',
  key: 'auditLogs.params.key',
  method: 'auditLogs.params.method',
  scope: 'auditLogs.params.scope',
  route: 'auditLogs.params.route',
  success: 'auditLogs.params.success',
  reason: 'auditLogs.params.reason',
  provider: 'auditLogs.params.provider',
  provider_id: 'auditLogs.params.providerId',
  token_ref: 'auditLogs.params.tokenRef',
  ticket_id: 'auditLogs.params.ticketId',
  trade_no: 'auditLogs.params.tradeNo',
  group_slug: 'auditLogs.params.groupSlug',
  event_id: 'auditLogs.params.eventId',
  operation_id: 'auditLogs.params.operationId',
  expected_version: 'auditLogs.params.expectedVersion',
  inviter_id: 'auditLogs.params.inviterId',
  invited_user_id: 'auditLogs.params.invitedUserId',
  bindingType: 'auditLogs.params.bindingType',
  sourceId: 'auditLogs.params.sourceId',
  changed_fields: 'auditLogs.params.changedFields',
  domains: 'auditLogs.params.domains',
  confirmed: 'auditLogs.params.confirmed',
  notification_failed: 'auditLogs.params.notificationFailed',
}

export function auditLogSummary(log: AuditLogItem, t: Translate): string {
  const key = AUDIT_LOG_ACTION_LABEL_KEYS[log.action]
  if (key) return t(key, { ...log.params, method: log.request.method })
  return log.content || log.action || t('auditLogs.unknownAction')
}

const AUDIT_LOG_CATEGORY_LABEL_KEYS: Record<AuditLogCategory, string> = {
  operation: 'auditLogs.category.operation',
  security: 'auditLogs.category.security',
  login: 'auditLogs.category.login',
  access_token: 'auditLogs.category.accessToken',
}

function isKnownAuditLogCategory(
  category: string
): category is AuditLogCategory {
  return Object.hasOwn(AUDIT_LOG_CATEGORY_LABEL_KEYS, category)
}

/** i18n keys for the four categories model.ValidAuditCategory accepts. */
export function auditLogCategoryKey(category: string): string | null {
  return isKnownAuditLogCategory(category)
    ? AUDIT_LOG_CATEGORY_LABEL_KEYS[category]
    : null
}

/** Rows written before the NewAPI-core rebuild may carry other categories
 * (for example `system`); like upstream they show the stored string. */
export function auditLogCategoryLabel(category: string, t: Translate): string {
  const key = auditLogCategoryKey(category)
  return key ? t(key) : category
}

export function auditLogCategoryTone(
  category: string
): 'accent' | 'info' | 'warning' | 'neutral' {
  if (category === 'operation') return 'accent'
  if (category === 'login') return 'info'
  if (isKnownAuditLogCategory(category)) return 'warning'
  return 'neutral'
}

export function auditLogResultKey(log: AuditLogItem): string {
  return log.request.success
    ? 'auditLogs.result.success'
    : 'auditLogs.result.failed'
}

export function auditLogResultTone(log: AuditLogItem): 'success' | 'danger' {
  return log.request.success ? 'success' : 'danger'
}

export function auditLogRoleKey(role: number | null): string {
  if (role === null) return 'auditLogs.role.unknown'
  if (role >= 100) return 'auditLogs.role.root'
  if (role >= 10) return 'auditLogs.role.admin'
  return 'auditLogs.role.user'
}

export function auditLogAuthMethodKey(method: string): string | null {
  if (method === 'session') return 'auditLogs.auth.session'
  if (method === 'access_token') return 'auditLogs.auth.accessToken'
  if (method === 'password') return 'auditLogs.auth.password'
  if (method === '2fa') return 'auditLogs.auth.twoFactor'
  if (method === 'passkey') return 'auditLogs.auth.passkey'
  if (method.startsWith('oauth:')) return 'auditLogs.auth.oauth'
  return null
}

export function auditLogParamLabel(key: string, t: Translate): string {
  const labelKey = AUDIT_LOG_PARAM_LABEL_KEYS[key]
  return labelKey ? t(labelKey) : key
}

export function auditLogParamValue(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (Array.isArray(value)) return value.map(String).join(', ')
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

export function auditLogRequestText(log: AuditLogItem): string {
  return [log.request.method, log.request.route].filter(Boolean).join(' ')
}
