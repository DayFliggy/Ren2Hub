import { flushPromises } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { ref } from 'vue'

const mocks = vi.hoisted(() => ({ list: vi.fn() }))
vi.mock('@/api/channelUpstreamRatio', () => ({
  channelUpstreamRatioApi: { list: mocks.list },
}))

import { useChannelUpstreamRatios } from '../useChannelUpstreamRatios'
import type { ChannelUpstreamRatio } from '@/api/channelUpstreamRatio'
import type { AdminChannel } from '@/types/console'

// After a failed reload the old values may belong to a changed channel, so they
// stay visible but must read as stale.
it('marks kept ratios stale when a reload fails', async () => {
  const view = { channel_id: 7, ratio: '1.5', stale: false }
  mocks.list.mockResolvedValueOnce([view])
  const rows = ref([{ id: 7 }] as AdminChannel[])
  const { ratios, error } = useChannelUpstreamRatios(rows, ref(true))
  await flushPromises()
  expect(ratios.value[7]?.stale).toBe(false)

  mocks.list.mockRejectedValueOnce(new Error('timeout'))
  rows.value = [{ id: 7 }] as AdminChannel[]
  await flushPromises()
  expect(ratios.value[7]).toMatchObject({ ratio: '1.5', stale: true })
  expect(error.value).toBe('Error: timeout')
})

// A dialog save can finish while a list request is in flight; the older list
// response must not overwrite the newer saved record.
it('keeps a newer saved record over an older in-flight list response', async () => {
  let resolveList: (value: unknown[]) => void = () => {}
  mocks.list.mockReturnValueOnce(
    new Promise((resolve) => {
      resolveList = resolve
    })
  )
  const rows = ref([{ id: 7 }, { id: 8 }] as AdminChannel[])
  const { ratios, update } = useChannelUpstreamRatios(rows, ref(true))
  update({ channel_id: 7, ratio: '2', version: 5 } as ChannelUpstreamRatio)
  resolveList([
    { channel_id: 7, ratio: '1.5', version: 4 },
    { channel_id: 8, ratio: '1', version: 1 },
  ])
  await flushPromises()
  expect(ratios.value[7]).toMatchObject({ ratio: '2', version: 5 })
  expect(ratios.value[8]).toMatchObject({ ratio: '1', version: 1 })
})

// A channel deleted and recreated with the same id starts again at version 1.
// Only saves made during a request are protected, so a plain reload must not
// keep the old channel's higher version forever.
it('accepts a lower version from a plain reload', async () => {
  mocks.list.mockResolvedValueOnce([
    { channel_id: 7, ratio: '1.5', version: 5 },
  ])
  const rows = ref([{ id: 7 }] as AdminChannel[])
  const { ratios } = useChannelUpstreamRatios(rows, ref(true))
  await flushPromises()
  expect(ratios.value[7]).toMatchObject({ version: 5 })

  mocks.list.mockResolvedValueOnce([{ channel_id: 7, ratio: '', version: 1 }])
  rows.value = [{ id: 7 }] as AdminChannel[]
  await flushPromises()
  expect(ratios.value[7]).toMatchObject({ ratio: '', version: 1 })
})

// Two reloads can overlap at a microtask boundary: the first result is applied
// after the second load has started. Without any dialog save, the final state
// must still be the latest response.
it('keeps the latest response when reloads overlap', async () => {
  for (let ticks = 0; ticks < 8; ticks++) {
    let resolveFirst: (value: unknown[]) => void = () => {}
    mocks.list.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveFirst = resolve
      })
    )
    const { ratios, reload } = useChannelUpstreamRatios(
      ref([{ id: 7 }] as AdminChannel[]),
      ref(true)
    )
    resolveFirst([{ channel_id: 7, ratio: '1.5', version: 5 }])
    for (let i = 0; i < ticks; i++) await Promise.resolve()
    mocks.list.mockResolvedValueOnce([{ channel_id: 7, ratio: '', version: 1 }])
    void reload()
    await flushPromises()
    expect(ratios.value[7], `ticks=${ticks}`).toMatchObject({ version: 1 })
  }
})

// A dialog save that lands while the list request is in flight is new data; a
// later failure of that request must not mark it stale.
it('does not mark a save made during a failed reload as stale', async () => {
  mocks.list.mockResolvedValueOnce([
    { channel_id: 7, ratio: '1.5', version: 3, stale: false },
    { channel_id: 8, ratio: '1', version: 1, stale: false },
  ])
  const { ratios, reload, update } = useChannelUpstreamRatios(
    ref([{ id: 7 }, { id: 8 }] as AdminChannel[]),
    ref(true)
  )
  await flushPromises()
  let rejectList: (error: Error) => void = () => {}
  mocks.list.mockReturnValueOnce(
    new Promise((_, reject) => {
      rejectList = reject
    })
  )
  void reload()
  update({
    channel_id: 7,
    ratio: '2',
    version: 4,
    stale: false,
  } as ChannelUpstreamRatio)
  rejectList(new Error('timeout'))
  await flushPromises()
  expect(ratios.value[7]).toMatchObject({ ratio: '2', stale: false })
  expect(ratios.value[8]).toMatchObject({ ratio: '1', stale: true })
})
