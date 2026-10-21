import { createPinia } from 'pinia'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import { useAdminRedemption } from '@/composables/useAdminRedemption'
import i18n from '@/i18n'
type AdminRedemptionPage = ReturnType<typeof redemptionPage>

interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
}

function deferred<T>(): Deferred<T> {
  let resolve: (value: T) => void = () => undefined
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

function redemptionPage(marker: number) {
  return {
    items: [
      {
        id: marker + 1,
        name: 'code',
        key: 'code-key',
        status: 1,
        quota: 500000,
        used_user_id: 0,
        created_time: 1,
        redeemed_time: 0,
        expired_time: 0,
      },
    ],
    total: 1,
    page: 1,
    page_size: 20,
    type_counts: {},
    status_counts: {},
  }
}

const wrappers: VueWrapper[] = []
let redemptionState: ReturnType<typeof useAdminRedemption> | null = null

const AdminListHost = defineComponent({
  setup() {
    redemptionState = useAdminRedemption()
    return () => null
  },
})

function setupRedemption(): ReturnType<typeof useAdminRedemption> {
  redemptionState = null
  wrappers.push(
    mount(AdminListHost, {
      global: { plugins: [createPinia(), i18n] },
    })
  )
  if (!redemptionState)
    throw new Error('expected redemption composable instance')
  return redemptionState
}

afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.restoreAllMocks()
})

describe('admin list request ordering', () => {
  it('keeps the newest redemption filter response when an older request lands late', async () => {
    const requests = [
      deferred<AdminRedemptionPage>(),
      deferred<AdminRedemptionPage>(),
      deferred<AdminRedemptionPage>(),
    ]
    vi.spyOn(api, 'get')
      .mockReturnValueOnce(requests[0].promise as never)
      .mockReturnValueOnce(requests[1].promise as never)
      .mockReturnValueOnce(requests[2].promise as never)

    const state = setupRedemption()
    requests[0].resolve(redemptionPage(0))
    await flushPromises()

    state.statusFilter.value = 'unused'
    await nextTick()
    state.statusFilter.value = 'disabled'
    await nextTick()

    requests[2].resolve(redemptionPage(2))
    await flushPromises()
    requests[1].resolve(redemptionPage(1))
    await flushPromises()

    expect(state.rows.value[0]?.id).toBe(3)
    expect(state.loading.value).toBe(false)
  })

  it('returns to the first page when the page size changes', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValue(redemptionPage(0) as never)
    const state = setupRedemption()
    await flushPromises()

    state.page.value = 3
    await nextTick()
    await flushPromises()
    state.pageSize.value = 50
    await nextTick()
    await flushPromises()

    expect(state.page.value).toBe(1)
    expect(get).toHaveBeenLastCalledWith(
      '/api/redemption/',
      expect.objectContaining({ p: 1, page_size: 50 }),
      expect.anything()
    )
  })
})
