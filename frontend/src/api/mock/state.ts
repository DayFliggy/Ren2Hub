import { clearDemoUser } from '@/api/demoStorage'

import * as bigame from './bigame'
import * as data from './data'
import * as farm from './farm'
import * as lab from './lab'

type MutableValue = Record<string, unknown> | unknown[]

export interface MockRuntime {
  nextTokenId: number
  nextTicketId: number
  nextMessageId: number
  nextListingId: number
  marketSelfEarnings: number
}

export interface MockState {
  latencyMs: number
  reset(): void
}

const modules = [data, farm, lab, bigame]

function isMutable(value: unknown): value is MutableValue {
  return Array.isArray(value) || (value !== null && typeof value === 'object')
}

function clone<T>(value: T): T {
  return structuredClone(value)
}

function captureSnapshots(): Map<MutableValue, MutableValue> {
  const snapshots = new Map<MutableValue, MutableValue>()
  for (const module of modules) {
    for (const value of Object.values(module)) {
      if (isMutable(value)) snapshots.set(value, clone(value))
    }
  }
  return snapshots
}

const snapshots = captureSnapshots()

export const mockRuntime: MockRuntime = {
  nextTokenId: 100,
  nextTicketId: 100,
  nextMessageId: 1_000,
  nextListingId: 5_000,
  marketSelfEarnings: 3_260_000,
}

export function createMockState(): MockState {
  const defaultLatencyMs = import.meta.env.MODE === 'test' ? 0 : 120
  const state: MockState = {
    latencyMs: defaultLatencyMs,
    reset() {
      for (const [target, source] of snapshots) {
        if (Array.isArray(target) && Array.isArray(source)) {
          target.splice(0, target.length, ...clone(source))
          continue
        }
        if (!Array.isArray(target) && !Array.isArray(source)) {
          for (const key of Object.keys(target)) delete target[key]
          Object.assign(target, clone(source))
        }
      }
      Object.assign(mockRuntime, {
        nextTokenId: 100,
        nextTicketId: 100,
        nextMessageId: 1_000,
        nextListingId: 5_000,
        marketSelfEarnings: 3_260_000,
      })
      data.resetMockDataCounters()
      state.latencyMs = defaultLatencyMs
      clearDemoUser()
    },
  }
  return state
}

export const mockState = createMockState()

export function getMockDelay(): number {
  return mockState.latencyMs
}

export function setMockDelay(delayMs: number): void {
  mockState.latencyMs = Math.max(0, Math.min(1_000, Math.round(delayMs)))
}

export function resetMockState(): void {
  mockState.reset()
}
