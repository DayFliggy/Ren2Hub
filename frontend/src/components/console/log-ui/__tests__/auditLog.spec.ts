import { beforeAll, describe, expect, it } from 'vitest'

import {
  AUDIT_LOG_ACTION_LABEL_KEYS,
  AUDIT_LOG_PARAM_LABEL_KEYS,
  auditLogAuthMethodKey,
  auditLogCategoryKey,
  auditLogParamLabel,
  auditLogParamValue,
  auditLogRoleKey,
  auditLogSummary,
} from '@/components/console/log-ui/auditLog'
import i18n, { loadMessageDomain } from '@/i18n'
import enConsole from '@/i18n/locales/en/console'
import zhConsole from '@/i18n/locales/zh-CN/console'
import type { AuditLogCategory, AuditLogItem } from '@/types/console'

type Messages = Record<string, unknown>

const sources = import.meta.glob<string>(
  [
    '@/views/console/AuditLogsView.vue',
    '@/components/console/log-ui/auditLog.ts',
  ],
  { query: '?raw', import: 'default', eager: true }
)

function resolve(messages: Messages, path: string): unknown {
  return path
    .split('.')
    .reduce<unknown>(
      (node, part) =>
        node && typeof node === 'object' ? (node as Messages)[part] : undefined,
      messages
    )
}

function missingIn(keys: Iterable<string>): string[] {
  return [...keys].filter(
    (key) =>
      typeof resolve(zhConsole as Messages, key) !== 'string' ||
      typeof resolve(enConsole as Messages, key) !== 'string'
  )
}

function auditRow(overrides: Partial<AuditLogItem> = {}): AuditLogItem {
  return {
    event_id: 'evt-1',
    created_at: 1_700_000_000,
    category: 'operation',
    action: '',
    params: {},
    content: '',
    actor: { id: 1, username: 'alice', role: 1, auth_method: 'session' },
    token_ref: '',
    request_id: '',
    ip: '',
    user_agent: '',
    request: { method: 'POST', route: '', status: 200, success: true },
    ...overrides,
  }
}

const t = (key: string, params?: Record<string, unknown>) =>
  i18n.global.t(key, params ?? {})

beforeAll(async () => {
  i18n.global.locale.value = 'zh-CN'
  await loadMessageDomain('console')
})

describe('audit log labels resolve in both locales', () => {
  it('covers every action and parameter label key', () => {
    expect(missingIn(Object.values(AUDIT_LOG_ACTION_LABEL_KEYS))).toEqual([])
    expect(missingIn(Object.values(AUDIT_LOG_PARAM_LABEL_KEYS))).toEqual([])
  })

  it('covers the computed category, role, and auth method keys', () => {
    const categories: AuditLogCategory[] = [
      'login',
      'security',
      'operation',
      'access_token',
      'system',
    ]
    const methods = ['session', 'access_token', 'password', '2fa', 'passkey']
    expect(
      missingIn([
        ...categories.map(auditLogCategoryKey),
        ...[null, 1, 10, 100].map(auditLogRoleKey),
        ...[...methods, 'oauth:github'].map((method) =>
          auditLogAuthMethodKey(method)!
        ),
      ])
    ).toEqual([])
    expect(auditLogAuthMethodKey('unknown')).toBeNull()
  })

  it('resolves every literal auditLogs.* key used by the page', () => {
    const referenced = new Set<string>()
    for (const source of Object.values(sources)) {
      for (const match of source.matchAll(
        /['"`](auditLogs\.[a-zA-Z0-9_.]+)['"`]/g
      )) {
        referenced.add(match[1]!)
      }
    }
    // A scan that stopped matching would let the next assertion pass vacuously.
    expect(referenced.size).toBeGreaterThan(40)
    expect(missingIn(referenced)).toEqual([])
  })
})

describe('audit log summaries', () => {
  it('renders mapped actions with their recorded parameters', () => {
    expect(
      auditLogSummary(
        auditRow({ action: 'user.update', params: { username: 'bob' } }),
        t
      )
    ).toBe('更新用户 bob')
    expect(auditLogSummary(auditRow({ action: 'login' }), t)).toBe(
      '账号登录成功'
    )
  })

  it('falls back to content, then the raw action, then a placeholder', () => {
    expect(
      auditLogSummary(
        auditRow({ action: 'plugin.custom', content: 'Legacy text' }),
        t
      )
    ).toBe('Legacy text')
    expect(auditLogSummary(auditRow({ action: 'plugin.custom' }), t)).toBe(
      'plugin.custom'
    )
    expect(auditLogSummary(auditRow(), t)).toBe('未知操作')
  })

  it('labels known parameters and formats structured values', () => {
    expect(auditLogParamLabel('notification_failed', t)).toBe('通知发送失败')
    expect(auditLogParamLabel('custom_field', t)).toBe('custom_field')
    expect(auditLogParamValue(['a', 2])).toBe('a, 2')
    expect(auditLogParamValue({ a: 1 })).toBe('{"a":1}')
    expect(auditLogParamValue(null)).toBe('-')
  })
})
