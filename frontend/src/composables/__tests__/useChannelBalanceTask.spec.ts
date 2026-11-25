import { afterEach, describe, expect, it, vi } from 'vitest'
import { effectScope } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import { clearAuthBundle } from '@/api/authSession'
import { api } from '@/api/client'
import { useChannelBalanceTask } from '@/composables/useChannelBalanceTask'

function mountComposable(onComplete = vi.fn()) {
  let exposed!: ReturnType<typeof useChannelBalanceTask>
  const Host = defineComponent({
    setup() {
      exposed = useChannelBalanceTask({ onComplete })
      return () => null
    },
  })
  const wrapper = mount(Host, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  return { wrapper, state: exposed, onComplete }
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe('useChannelBalanceTask', () => {
  it('polls until completion, invokes onComplete once, and supports dismiss', async () => {
    vi.useFakeTimers()
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({ task_id: 't1', status: 'pending' })
      .mockResolvedValueOnce({
        task_id: 't1',
        status: 'running',
        state: { processed: 2, total: 4 },
      })
      .mockResolvedValueOnce({
        task_id: 't1',
        status: 'succeeded',
        state: { processed: 4, total: 4 },
      })

    const { wrapper, state, onComplete } = mountComposable()
    await state.start()
    expect(state.active.value).toBe(true)
    // Duplicate start while active is ignored
    await state.start()
    expect(get).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(state.task.value?.state).toEqual({ processed: 2, total: 4 })
    // Dismiss while active is a no-op
    state.dismiss()
    expect(state.task.value).not.toBeNull()

    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(state.active.value).toBe(false)
    expect(state.task.value?.status).toBe('succeeded')
    expect(onComplete).toHaveBeenCalledTimes(1)

    state.dismiss()
    expect(state.task.value).toBeNull()
    wrapper.unmount()
  })

  it('ignores late responses after auth session generation changes and aborts on unmount', async () => {
    vi.useFakeTimers()
    let resolveStart!: (value: unknown) => void
    const get = vi.spyOn(api, 'get').mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveStart = resolve
        }) as never
    )

    const { wrapper, state, onComplete } = mountComposable()
    const pending = state.start()
    const signal = (get.mock.calls[0]![2] as { signal: AbortSignal }).signal

    // Switch session generation before response arrives
    clearAuthBundle()
    resolveStart({ task_id: 'stale', status: 'succeeded' })
    await pending
    await flushPromises()

    expect(state.task.value).toBeNull()
    expect(state.busy.value).toBe(false)
    expect(onComplete).not.toHaveBeenCalled()

    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    // Sanity check effectScope doesn't leak timers
    const scope = effectScope()
    scope.stop()
  })

  it('handles null payload cleanly and cancels active polling when permissions are revoked', async () => {
    vi.useFakeTimers()
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce(null as unknown)
      .mockResolvedValueOnce({ task_id: 't9', status: 'running' })

    const { wrapper, state } = mountComposable()
    await state.start()
    expect(state.error.value).toBe('pluginExtensions.error')
    expect(state.task.value).toBeNull()
    state.dismiss()

    await state.start()
    expect(state.active.value).toBe(true)
    state.cancel()
    expect(state.active.value).toBe(false)
    expect(state.task.value).toBeNull()
    await vi.advanceTimersByTimeAsync(4000)
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('recovers from start rejection, poll failure with retry or dismiss, failed status, and invalid payloads', async () => {
    vi.useFakeTimers()
    const get = vi
      .spyOn(api, 'get')
      // 1. start() rejects
      .mockRejectedValueOnce(new Error('network down'))
      // 2. start() succeeds pending, first poll rejects, retry poll succeeds
      .mockResolvedValueOnce({ task_id: 't2', status: 'pending' })
      .mockRejectedValueOnce(new Error('gateway timeout'))
      .mockResolvedValueOnce({ task_id: 't2', status: 'succeeded' })
      // 3. start() succeeds running, poll rejects, dismiss resets deadlock so start() works again
      .mockResolvedValueOnce({ task_id: 't3', status: 'running' })
      .mockRejectedValueOnce(new Error('task vanished'))
      // 4. start() returns backend failed status
      .mockResolvedValueOnce({
        task_id: 't4',
        status: 'failed',
        error: 'upstream error',
      })
      // 5. malformed payload throws localized error
      .mockResolvedValueOnce({ task_id: '', status: 'unknown' })

    const { wrapper, state, onComplete } = mountComposable()

    // Case 1: start rejection
    await state.start()
    expect(state.error.value).toBe('network down')
    expect(state.active.value).toBe(false)
    state.dismiss()
    expect(state.error.value).toBe('')

    // Case 2: poll failure -> manual retry via poll() succeeds without re-issuing start
    await state.start()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(state.error.value).toBe('gateway timeout')
    expect(state.active.value).toBe(true)
    await state.poll()
    expect(state.error.value).toBe('')
    expect(state.task.value?.status).toBe('succeeded')
    expect(onComplete).toHaveBeenCalledTimes(1)
    state.dismiss()

    // Case 3: poll failure -> dismiss clears stuck active task so a new start can run
    await state.start()
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(state.error.value).toBe('task vanished')
    expect(state.active.value).toBe(true)
    state.dismiss()
    expect(state.task.value).toBeNull()
    expect(state.error.value).toBe('')
    expect(state.active.value).toBe(false)

    // Case 4: backend failed status
    await state.start()
    expect(state.task.value?.status).toBe('failed')
    expect(state.error.value).toBe('upstream error')
    expect(state.active.value).toBe(false)
    state.dismiss()

    // Case 5: malformed payload
    await state.start()
    expect(state.error.value).toBe('pluginExtensions.error')
    expect(state.task.value).toBeNull()

    expect(
      get.mock.calls.filter((call) => call[0] === '/api/channel/update_balance')
    ).toHaveLength(5)
    wrapper.unmount()
  })
})
