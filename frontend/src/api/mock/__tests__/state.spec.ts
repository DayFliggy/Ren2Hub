import { afterEach, describe, expect, it } from 'vitest'

import { writeDemoUser } from '@/api/demoStorage'
import { tokens } from '@/api/mock/data'
import { getMockDelay, resetMockState, setMockDelay } from '@/api/mock/state'

const demoUser = {
  id: 1,
  username: 'demo',
  display_name: 'Demo',
  email: 'demo@example.com',
  role: 1,
  quota: 100,
  used_quota: 0,
  group: 'default',
}

afterEach(() => resetMockState())

describe('mock state reset', () => {
  it('restores fixtures, counters, delay and identity', () => {
    const originalName = tokens[0]?.name
    if (!tokens[0]) throw new Error('expected seeded token')
    tokens[0].name = 'mutated'
    writeDemoUser(demoUser)
    setMockDelay(500)

    resetMockState()

    expect(tokens[0].name).toBe(originalName)
    expect(getMockDelay()).toBe(0)
    expect(localStorage.length).toBe(0)
  })
})
