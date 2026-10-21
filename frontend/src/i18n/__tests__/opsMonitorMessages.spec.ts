import { describe, expect, it } from 'vitest'

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
})
