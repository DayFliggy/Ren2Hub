import type { LocaleCode } from '@/i18n'
import type { LogItem } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { logEvidence } from '@/utils/logModel'
import { logContentText } from './logContent'

export const CALL_LOG_TYPES_PARAM = '2,5'
export const QUOTA_LOG_TYPES_PARAM = '1,4,6'

export type CallLogSubkind =
  'violation_fee' | 'channel_test' | 'task_billing' | null

export interface LinkedTaskTarget {
  routeName: 'logs-tasks' | 'logs-drawing'
  query: Record<string, string>
  taskId: string
}

export type QuotaEntryKind =
  | 'online_topup'
  | 'redemption'
  | 'admin_add'
  | 'admin_subtract'
  | 'admin_override'
  | 'task_refund'
  | 'task_settlement_refund'
  | 'checkin'
  | 'signup_bonus'
  | 'topup'
  | 'refund'
  | 'system'

export interface QuotaEntrySummary {
  kind: QuotaEntryKind
  kindLabelKey: string
  tone: 'success' | 'warning' | 'danger' | 'info' | 'neutral'
  amountText: string | null
  amountTone: 'success' | 'danger' | 'warning' | 'neutral'
  linkedTask: LinkedTaskTarget | null
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function asNonEmptyString(value: unknown): string {
  return typeof value === 'string' && value.trim() ? value.trim() : ''
}

function asBigInt(value: unknown): bigint | null {
  if (typeof value === 'bigint') return value
  if (typeof value === 'number' && Number.isSafeInteger(value)) {
    return BigInt(value)
  }
  if (typeof value === 'string' && /^-?\d+$/.test(value.trim())) {
    return BigInt(value.trim())
  }
  return null
}

function firstContentPart(
  evidence: Record<string, unknown>
): { key: string; params: Record<string, unknown> } | null {
  const parts = evidence.content_parts
  if (!Array.isArray(parts) || parts.length === 0) return null
  const first = parts[0]
  if (!isRecord(first) || typeof first.key !== 'string') return null
  return {
    key: first.key,
    params: isRecord(first.params) ? first.params : {},
  }
}

function isMidjourneyModel(model: string): boolean {
  const lower = model.trim().toLowerCase()
  return lower.startsWith('mj_') || lower === 'swap_face'
}

/**
 * Resolves a cross-link from a call or refund log row to its originating async
 * task (Midjourney or general task) whenever the backend persisted `other.task_id`.
 */
export function logLinkedTask(
  log: Pick<LogItem, 'model' | 'other'>
): LinkedTaskTarget | null {
  const evidence = logEvidence(log as LogItem)
  const taskId = asNonEmptyString(evidence.task_id)
  if (!taskId) return null
  if (isMidjourneyModel(log.model)) {
    return {
      routeName: 'logs-drawing',
      query: { mj_id: taskId },
      taskId,
    }
  }
  return {
    routeName: 'logs-tasks',
    query: { task_id: taskId },
    taskId,
  }
}

/**
 * Classifies special call log rows (violation fee, channel test, async task
 * billing) without changing the underlying `consume` / `error` status.
 */
export function logCallSubkind(
  log: Pick<LogItem, 'type' | 'token_id' | 'token_name' | 'model' | 'other'>
): CallLogSubkind {
  const evidence = logEvidence(log as LogItem)
  if (evidence.violation_fee === true) return 'violation_fee'
  if (log.token_id === 0 && log.token_name === 'Model test') {
    return 'channel_test'
  }
  if (evidence.is_task === true || asNonEmptyString(evidence.task_id) !== '') {
    return 'task_billing'
  }
  return null
}

const QUOTA_KIND_LABELS: Record<QuotaEntryKind, string> = {
  online_topup: 'quotaLogs.kinds.onlineTopup',
  redemption: 'quotaLogs.kinds.redemption',
  admin_add: 'quotaLogs.kinds.adminAdd',
  admin_subtract: 'quotaLogs.kinds.adminSubtract',
  admin_override: 'quotaLogs.kinds.adminOverride',
  task_refund: 'quotaLogs.kinds.taskRefund',
  task_settlement_refund: 'quotaLogs.kinds.taskSettlementRefund',
  checkin: 'quotaLogs.kinds.checkin',
  signup_bonus: 'quotaLogs.kinds.signupBonus',
  topup: 'quotaLogs.kinds.topup',
  refund: 'quotaLogs.kinds.refund',
  system: 'quotaLogs.kinds.system',
}

function prefixSign(text: string, sign: '+' | '-'): string {
  if (!text) return text
  if (text.startsWith('+') || text.startsWith('-')) return text
  return `${sign}${text}`
}

/**
 * Derives the semantic ledger category and accurate quota delta for non-call
 * `logs` rows (`topup`, `refund`, `system`), avoiding false `$0` amounts when
 * the backend stores the formatted amount in `other.content_parts` or `other.op`.
 */
export function classifyQuotaLog(log: LogItem): QuotaEntrySummary {
  const evidence = logEvidence(log)
  const linkedTask = logLinkedTask(log)
  const op = isRecord(evidence.op) ? evidence.op : null
  const opAction = op ? asNonEmptyString(op.action) : ''
  const opParams = op && isRecord(op.params) ? op.params : {}
  const part = firstContentPart(evidence)
  const partKey = part?.key ?? ''
  const partQuotaText = part ? asNonEmptyString(part.params.quota) : ''
  const rowQuota = asBigInt(log.quota) ?? 0n

  // 1. Admin manual quota adjustments written as LogTypeTopup with other.op
  if (opAction === 'user.quota_subtract') {
    const delta = asBigInt(opParams.quota)
    const amountText =
      delta !== null && delta > 0n
        ? prefixSign(formatQuota(delta.toString()), '-')
        : rowQuota > 0n
          ? prefixSign(formatQuota(rowQuota.toString()), '-')
          : null
    return {
      kind: 'admin_subtract',
      kindLabelKey: QUOTA_KIND_LABELS.admin_subtract,
      tone: 'danger',
      amountText,
      amountTone: 'danger',
      linkedTask,
    }
  }

  if (opAction === 'user.quota_override') {
    const fromVal = asBigInt(opParams.from)
    const toVal = asBigInt(opParams.to)
    const amountText =
      fromVal !== null && toVal !== null
        ? `${formatQuota(fromVal.toString())} → ${formatQuota(toVal.toString())}`
        : null
    return {
      kind: 'admin_override',
      kindLabelKey: QUOTA_KIND_LABELS.admin_override,
      tone: 'warning',
      amountText,
      amountTone: 'warning',
      linkedTask,
    }
  }

  if (opAction === 'user.quota_add') {
    const base = asBigInt(opParams.quota)
    const bonus = asBigInt(opParams.bonus_quota) ?? 0n
    const total =
      base !== null && base > 0n ? base + (bonus > 0n ? bonus : 0n) : null
    const amountText =
      total !== null
        ? prefixSign(formatQuota(total.toString()), '+')
        : rowQuota > 0n
          ? prefixSign(formatQuota(rowQuota.toString()), '+')
          : null
    return {
      kind: 'admin_add',
      kindLabelKey: QUOTA_KIND_LABELS.admin_add,
      tone: 'success',
      amountText,
      amountTone: 'success',
      linkedTask,
    }
  }

  // 2. Refunds (task failures & async difference settlements)
  if (log.type === 'refund') {
    const isSettlement = evidence.pre_consumed_quota !== undefined
    const kind: QuotaEntryKind = isSettlement
      ? 'task_settlement_refund'
      : linkedTask
        ? 'task_refund'
        : 'refund'
    const amountText =
      rowQuota > 0n
        ? prefixSign(formatQuota(rowQuota.toString()), '+')
        : partQuotaText
          ? prefixSign(partQuotaText, '+')
          : null
    return {
      kind,
      kindLabelKey: QUOTA_KIND_LABELS[kind],
      tone: 'warning',
      amountText,
      amountTone: 'success',
      linkedTask,
    }
  }

  // 3. System rewards (daily check-in, sign-up bonus)
  if (log.type === 'system') {
    let kind: QuotaEntryKind = 'system'
    let tone: QuotaEntrySummary['tone'] = 'info'
    if (partKey.startsWith('Daily check-in')) {
      kind = 'checkin'
      tone = 'success'
    } else if (partKey.startsWith('New user sign-up bonus')) {
      kind = 'signup_bonus'
      tone = 'info'
    }
    const amountText =
      rowQuota > 0n
        ? prefixSign(formatQuota(rowQuota.toString()), '+')
        : partQuotaText
          ? prefixSign(partQuotaText, '+')
          : null
    return {
      kind,
      kindLabelKey: QUOTA_KIND_LABELS[kind],
      tone,
      amountText,
      amountTone: amountText ? 'success' : 'neutral',
      linkedTask,
    }
  }

  // 4. Online top-ups & redemptions
  let kind: QuotaEntryKind = 'topup'
  if (partKey.includes('redemption code')) {
    kind = 'redemption'
  } else if (
    partKey.includes('Online top-up') ||
    partKey.includes('Stripe') ||
    partKey.includes('Creem') ||
    partKey.includes('Waffo') ||
    partKey.includes('GMPay') ||
    partKey.toLowerCase().includes('top-up')
  ) {
    kind = 'online_topup'
  }
  const amountText =
    rowQuota > 0n
      ? prefixSign(formatQuota(rowQuota.toString()), '+')
      : partQuotaText
        ? prefixSign(partQuotaText, '+')
        : null
  return {
    kind,
    kindLabelKey: QUOTA_KIND_LABELS[kind],
    tone: 'success',
    amountText,
    amountTone: amountText ? 'success' : 'neutral',
    linkedTask,
  }
}

export interface TaskPlatformPreset {
  value: string
  label: string
}

/**
 * Known async task platform keys aligned with `relay/relay_adaptor.go`.
 * Custom plugin keys or historical channel type numbers stay supported via the
 * custom-platform option.
 */
export const TASK_PLATFORM_PRESETS: readonly TaskPlatformPreset[] = [
  { value: 'sunoapi', label: 'Suno (sunoapi)' },
  { value: 'suno', label: 'Suno (legacy)' },
  { value: 'sora', label: 'OpenAI Sora' },
  { value: 'kling', label: 'Kling' },
  { value: 'jimeng', label: 'Jimeng' },
  { value: 'vidu', label: 'Vidu' },
  { value: 'doubao', label: 'Doubao' },
  { value: 'hailuo', label: 'MiniMax Hailuo' },
  { value: 'alibaba', label: 'Alibaba Wanx' },
  { value: 'google', label: 'Google Veo' },
  { value: 'vertex-ai', label: 'Vertex AI' },
  { value: 'xai', label: 'xAI' },
]

const LEGACY_CHANNEL_PLATFORM_NAMES: Record<string, string> = {
  '1': 'OpenAI Sora',
  '17': 'Alibaba Wanx',
  '24': 'Google Veo',
  '35': 'MiniMax Hailuo',
  '41': 'Vertex AI',
  '45': 'VolcEngine Doubao',
  '49': 'Kling',
  '50': 'Jimeng',
  '51': 'Vidu',
  '52': 'Doubao Video',
  '53': 'OpenAI Sora',
}

const PRESET_LABEL_MAP = new Map(
  TASK_PLATFORM_PRESETS.map((item) => [item.value, item.label])
)

export function taskPlatformLabel(platform: string): string {
  const key = platform.trim()
  if (!key) return '-'
  return PRESET_LABEL_MAP.get(key) ?? LEGACY_CHANNEL_PLATFORM_NAMES[key] ?? key
}

type Translate = (key: string, params?: Record<string, unknown>) => string

/**
 * Formats a human-readable localized description for quota ledger entries.
 * Structured `other.op` admin quota adjustments are localized with formatted
 * currency instead of displaying raw integer quota units from English fallback text.
 */
export function quotaLogDescription(
  log: LogItem,
  locale: LocaleCode,
  t: Translate
): string {
  const evidence = logEvidence(log)
  const op = isRecord(evidence.op) ? evidence.op : null
  const opAction = op ? asNonEmptyString(op.action) : ''
  const opParams = op && isRecord(op.params) ? op.params : {}

  if (opAction === 'user.quota_subtract') {
    const delta = asBigInt(opParams.quota)
    if (delta !== null && delta > 0n) {
      return t('quotaLogs.descriptions.adminSubtract', {
        quota: formatQuota(delta.toString()),
      })
    }
  }
  if (opAction === 'user.quota_add') {
    const base = asBigInt(opParams.quota)
    const bonus = asBigInt(opParams.bonus_quota) ?? 0n
    if (base !== null && base > 0n) {
      if (bonus > 0n) {
        return t('quotaLogs.descriptions.adminAddBonus', {
          quota: formatQuota((base + bonus).toString()),
          bonus: formatQuota(bonus.toString()),
        })
      }
      return t('quotaLogs.descriptions.adminAdd', {
        quota: formatQuota(base.toString()),
      })
    }
  }
  if (opAction === 'user.quota_override') {
    const fromVal = asBigInt(opParams.from)
    const toVal = asBigInt(opParams.to)
    if (fromVal !== null && toVal !== null) {
      return t('quotaLogs.descriptions.adminOverride', {
        from: formatQuota(fromVal.toString()),
        to: formatQuota(toVal.toString()),
      })
    }
  }
  return logContentText(log, locale)
}
