import { describe, expect, it } from 'vitest'

import {
  OPS_BREAKER_STATES,
  OPS_CHECK_KEYS,
  OPS_PROBE_ERRORS,
  OPS_STATUSES,
  OPS_WINDOWS,
} from '@/api/opsMonitor'
import { SYSTEM_TASK_HEALTH } from '@/api/systemManagement'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { systemManagementMessages } from '@/i18n/systemManagement'

function keys(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) return [prefix]
  return Object.entries(value).flatMap(([key, child]) =>
    keys(child, prefix ? `${prefix}.${key}` : key)
  )
}

describe('ops monitor messages', () => {
  it('keeps zh-CN and en in step', () => {
    expect(keys(opsMonitorMessages.en).sort()).toEqual(
      keys(opsMonitorMessages['zh-CN']).sort()
    )
    expect(keys(systemManagementMessages.en).sort()).toEqual(
      keys(systemManagementMessages['zh-CN']).sort()
    )
  })

  it('labels every status, check, error, window and breaker state', () => {
    const labels = new Set(keys(opsMonitorMessages['zh-CN']))
    const expected = [
      ...OPS_STATUSES.map((status) => `status.${status}`),
      ...OPS_CHECK_KEYS.map((key) => `checks.${key}`),
      ...OPS_PROBE_ERRORS.map((error) => `errors.${error}`),
      ...OPS_WINDOWS.map((window) => `window.${window}`),
      ...OPS_BREAKER_STATES.map((state) => `upstream.${state}`),
      ...SYSTEM_TASK_HEALTH.map((health) => `tasks.health.${health}`),
    ]
    expect(expected.filter((key) => !labels.has(key))).toEqual([])
  })
})
