import { describe, expect, it } from 'vitest'

import { parseLogPage } from '@/api/liveContracts'
import {
  classifyQuotaLog,
  logCallSubkind,
  logLinkedTask,
  taskPlatformLabel,
} from '@/components/console/log-ui/logClassification'
import { getLogUsageSummary } from '@/components/console/log-ui/logUsage'
import type { LogItem } from '@/types/console'

function rawLogRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    type: 2,
    token_id: 5,
    token_name: 'main-key',
    model_name: 'gpt-4o',
    channel_name: '',
    prompt_tokens: 0,
    completion_tokens: 0,
    quota: 0,
    use_time: 0,
    is_stream: false,
    content: '',
    other: '{}',
    created_at: 1_750_000_000,
    ...overrides,
  }
}

describe('parseLogPage non-call request_mode normalization', () => {
  it('sets request_mode to null for topup, refund, and system logs so usage cells stay empty', () => {
    const page = parseLogPage({
      page: 1,
      page_size: 10,
      total: 5,
      items: [
        rawLogRow({
          id: 1,
          type: 2,
          is_stream: true,
          prompt_tokens: 10,
          completion_tokens: 5,
        }),
        rawLogRow({ id: 2, type: 5, is_stream: false }),
        rawLogRow({ id: 3, type: 1, is_stream: false }),
        rawLogRow({ id: 4, type: 4, is_stream: false }),
        rawLogRow({ id: 5, type: 6, is_stream: false, quota: 250000 }),
      ],
    })

    expect(page.items[0]!.request_mode).toBe('stream')
    expect(page.items[1]!.request_mode).toBe('sync')
    expect(page.items[2]!.request_mode).toBeNull()
    expect(page.items[3]!.request_mode).toBeNull()
    expect(page.items[4]!.request_mode).toBeNull()

    expect(getLogUsageSummary(page.items[0]!).available).toBe(true)
    expect(getLogUsageSummary(page.items[2]!).available).toBe(false)
    expect(getLogUsageSummary(page.items[3]!).available).toBe(false)
    expect(getLogUsageSummary(page.items[4]!).available).toBe(false)
  })

  it('preserves admin username, channel_id, and upstream_request_id when present', () => {
    const page = parseLogPage(
      {
        page: 1,
        page_size: 10,
        total: 1,
        items: [
          rawLogRow({
            username: 'alice',
            channel: 12,
            channel_name: 'Primary OpenAI',
            upstream_request_id: 'up-req-9',
          }),
        ],
      },
      '/api/log/'
    )
    expect(page.items[0]).toMatchObject({
      username: 'alice',
      channel_id: 12,
      channel: 'Primary OpenAI',
      upstream_request_id: 'up-req-9',
    })
  })
})

describe('classifyQuotaLog', () => {
  function parseOne(overrides: Record<string, unknown>): LogItem {
    return parseLogPage({
      page: 1,
      page_size: 1,
      total: 1,
      items: [rawLogRow(overrides)],
    }).items[0]!
  }

  it('extracts online top-up and redemption amounts from content_parts without showing $0', () => {
    const online = classifyQuotaLog(
      parseOne({
        type: 1,
        quota: 0,
        other: JSON.stringify({
          content_parts: [
            {
              key: 'Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}',
              params: { quota: '$10.00', amount: '72.00' },
            },
          ],
        }),
      })
    )
    expect(online.kind).toBe('online_topup')
    expect(online.tone).toBe('success')
    expect(online.amountText).toBe('+$10.00')

    const redemption = classifyQuotaLog(
      parseOne({
        type: 1,
        quota: 0,
        other: JSON.stringify({
          content_parts: [
            {
              key: 'Topped up {{quota}} with a redemption code, code ID {{id}}',
              params: { quota: '$5.00', id: 42 },
            },
          ],
        }),
      })
    )
    expect(redemption.kind).toBe('redemption')
    expect(redemption.amountText).toBe('+$5.00')
  })

  it('distinguishes admin add, subtract, and override adjustments with accurate signs and tones', () => {
    const add = classifyQuotaLog(
      parseOne({
        type: 1,
        quota: 0,
        other: JSON.stringify({
          op: {
            action: 'user.quota_add',
            params: { quota: 500000, bonus_quota: 100000 },
          },
        }),
      })
    )
    expect(add.kind).toBe('admin_add')
    expect(add.tone).toBe('success')
    expect(add.amountTone).toBe('success')
    expect(add.amountText).toBe('+$1.20')

    const subtract = classifyQuotaLog(
      parseOne({
        type: 1,
        quota: 0,
        other: JSON.stringify({
          op: { action: 'user.quota_subtract', params: { quota: 1000000 } },
        }),
      })
    )
    expect(subtract.kind).toBe('admin_subtract')
    expect(subtract.tone).toBe('danger')
    expect(subtract.amountTone).toBe('danger')
    expect(subtract.amountText).toBe('-$2.00')

    const override = classifyQuotaLog(
      parseOne({
        type: 1,
        quota: 0,
        other: JSON.stringify({
          op: {
            action: 'user.quota_override',
            params: { from: 500000, to: 2500000 },
          },
        }),
      })
    )
    expect(override.kind).toBe('admin_override')
    expect(override.tone).toBe('warning')
    expect(override.amountText).toBe('$1.00 → $5.00')
  })

  it('classifies daily check-in, sign-up bonus, task refund, settlement refund, and unknown rows', () => {
    const checkin = classifyQuotaLog(
      parseOne({
        type: 4,
        quota: 0,
        other: JSON.stringify({
          content_parts: [
            {
              key: 'Daily check-in, received {{quota}}',
              params: { quota: '$0.25' },
            },
          ],
        }),
      })
    )
    expect(checkin.kind).toBe('checkin')
    expect(checkin.amountText).toBe('+$0.25')

    const signup = classifyQuotaLog(
      parseOne({
        type: 4,
        quota: 0,
        other: JSON.stringify({
          content_parts: [
            {
              key: 'New user sign-up bonus {{quota}}',
              params: { quota: '$1.00' },
            },
          ],
        }),
      })
    )
    expect(signup.kind).toBe('signup_bonus')
    expect(signup.amountText).toBe('+$1.00')

    const mjRefund = classifyQuotaLog(
      parseOne({
        type: 6,
        model_name: 'mj_imagine',
        quota: 500000,
        other: JSON.stringify({ task_id: 'mj-task-88', reason: 'timeout' }),
      })
    )
    expect(mjRefund.kind).toBe('task_refund')
    expect(mjRefund.amountText).toBe('+$1.00')
    expect(mjRefund.linkedTask).toEqual({
      routeName: 'logs-drawing',
      query: { mj_id: 'mj-task-88' },
      taskId: 'mj-task-88',
    })

    const settlement = classifyQuotaLog(
      parseOne({
        type: 6,
        model_name: 'sora-2',
        quota: 250000,
        other: JSON.stringify({
          task_id: 'task_abc123',
          pre_consumed_quota: 750000,
          actual_quota: 500000,
        }),
      })
    )
    expect(settlement.kind).toBe('task_settlement_refund')
    expect(settlement.linkedTask).toEqual({
      routeName: 'logs-tasks',
      query: { task_id: 'task_abc123' },
      taskId: 'task_abc123',
    })

    const unknownZero = classifyQuotaLog(parseOne({ type: 1, quota: 0 }))
    expect(unknownZero.amountText).toBeNull()
  })
})

describe('call subkinds, task links, and platform labels', () => {
  it('identifies violation fee, channel test, task billing, and platform names', () => {
    expect(
      logCallSubkind({
        type: 'consume',
        token_id: 2,
        token_name: 'k',
        model: 'grok-2',
        other: { violation_fee: true },
      })
    ).toBe('violation_fee')
    expect(
      logCallSubkind({
        type: 'consume',
        token_id: 0,
        token_name: 'Model test',
        model: 'gpt-4o',
        other: null,
      })
    ).toBe('channel_test')
    expect(
      logCallSubkind({
        type: 'consume',
        token_id: 2,
        token_name: 'k',
        model: 'sora-2',
        other: { is_task: true, task_id: 'task_1' },
      })
    ).toBe('task_billing')
    expect(
      logLinkedTask({ model: 'swap_face', other: { task_id: 'mj_swap_1' } })
    ).toEqual({
      routeName: 'logs-drawing',
      query: { mj_id: 'mj_swap_1' },
      taskId: 'mj_swap_1',
    })
    expect(taskPlatformLabel('sunoapi')).toBe('Suno (sunoapi)')
    expect(taskPlatformLabel('45')).toBe('VolcEngine Doubao')
    expect(taskPlatformLabel('custom-plugin')).toBe('custom-plugin')
  })
})
