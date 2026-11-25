import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import type { UpstreamAccount } from '@/api/upstreams'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { accountAlert, nextSyncText, siteHost } from '../upstreamText'
import fixture from '../../../../../tests/fixtures/native/upstreams.get.json'

const t = (key: string, values?: Record<string, unknown>) =>
  i18n.global.t(key, values ?? {})
const te = (key: string) => i18n.global.te(key)

function account(patch: Partial<UpstreamAccount> = {}): UpstreamAccount {
  const base = structuredClone(fixture.list.data[0]) as unknown as Omit<
    UpstreamAccount,
    'site_meta'
  > & { site_meta: Partial<UpstreamAccount['site_meta']> }
  return { ...base, ...patch } as UpstreamAccount
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

afterEach(() => {
  vi.useRealTimers()
})

describe('accountAlert', () => {
  it('maps every backend status to one banner', () => {
    expect(
      accountAlert(account({ status: 'credential_undecryptable' }), t, te)
    ).toMatchObject({
      tone: 'danger',
      title: t('upstreams.alerts.undecryptable'),
      action: 'reauth',
    })
    expect(
      accountAlert(account({ status: 'reauth_required' }), t, te)
    ).toMatchObject({
      tone: 'warning',
      title: t('upstreams.alerts.reauth'),
      action: 'reauth',
    })
    expect(accountAlert(account({ status: 'disabled' }), t, te)).toMatchObject({
      tone: 'neutral',
      title: t('upstreams.alerts.disabled'),
      action: 'enable',
    })
  })

  it('keeps an unknown status visible without a recovery action', () => {
    expect(accountAlert(account({ status: 'paused' }), t, te)).toEqual({
      tone: 'warning',
      title: t('upstreams.statuses.unknown', { status: 'paused' }),
      detail: '',
      action: null,
    })
  })

  it('reports a failed sync on an active account with the translated code', () => {
    expect(
      accountAlert(account({ last_error: 'upstream_unreachable' }), t, te)
    ).toEqual({
      tone: 'danger',
      title: t('upstreams.alerts.syncFailed'),
      detail: t('upstreams.errors.upstream_unreachable'),
      action: 'sync',
    })
  })

  it('shows server text for an unknown error code', () => {
    expect(
      accountAlert(account({ last_error: 'dial tcp: refused' }), t, te)?.detail
    ).toBe('dial tcp: refused')
  })

  it('has no banner for a healthy active account', () => {
    expect(accountAlert(account(), t, te)).toBeNull()
  })
})

describe('siteHost', () => {
  it('returns the host and keeps malformed addresses as stored', () => {
    expect(siteHost('https://api.example.com:8443/v1')).toBe(
      'api.example.com:8443'
    )
    expect(siteHost('not a url')).toBe('not a url')
  })
})

describe('nextSyncText', () => {
  const now = 1_800_000_000

  function at(nextSyncAt: number, status = 'active') {
    vi.useFakeTimers()
    vi.setSystemTime(now * 1000)
    return nextSyncText(
      account({ status, next_sync_at: nextSyncAt }),
      now,
      t,
      'zh-CN'
    )
  }

  it('hides the time for inactive accounts and unscheduled syncs', () => {
    expect(at(now + 1800, 'reauth_required')).toBe('')
    expect(at(0)).toBe('')
  })

  it('reads "soon" for overdue and imminent syncs', () => {
    expect(at(now - 180)).toBe(t('upstreams.syncDueSoon'))
    expect(at(now + 45)).toBe(t('upstreams.syncDueSoon'))
  })

  it('shows the future relative time', () => {
    expect(at(now + 1800)).toBe(t('upstreams.nextSync', { time: '30 分钟后' }))
  })
})
