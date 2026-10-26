import { defineComponent, h, reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  fetchSelf: vi.fn(),
}))
const app = reactive({ checkinEnabled: true })
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
import { useActivity, type CheckinStatus } from '@/composables/useActivity'

const status: CheckinStatus = {
  enabled: true,
  min_quota: 100,
  max_quota: 500,
  today: '2026-10-09',
  stats: {
    total_quota: 0,
    total_checkins: 0,
    checkin_count: 0,
    checked_in_today: false,
    records: [],
  },
}
beforeEach(() => {
  vi.clearAllMocks()
  app.checkinEnabled = true
  mocks.get.mockResolvedValue(status)
  mocks.post.mockResolvedValue(null)
  mocks.fetchSelf.mockResolvedValue(true)
})
function render() {
  let activity!: ReturnType<typeof useActivity>
  const wrapper = mount(
    defineComponent({
      setup() {
        activity = useActivity()
        return () => h('div')
      },
    })
  )
  return {
    wrapper,
    get activity() {
      return activity
    },
  }
}
describe('native daily check-in', () => {
  it('uses the native claim contract and refreshes balance and selected-month records', async () => {
    const state = render()
    await flushPromises()
    await state.activity.checkin('challenge')
    expect(mocks.post).toHaveBeenCalledWith('/api/user/checkin', undefined, {
      params: { turnstile: 'challenge' },
    })
    expect(mocks.fetchSelf).toHaveBeenCalledOnce()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/user/checkin',
      { month: state.activity.month.value },
      { signal: expect.any(AbortSignal) }
    )
    state.wrapper.unmount()
  })
  it('shows a failed claim and does not refresh the balance as if it succeeded', async () => {
    mocks.post.mockRejectedValue(new Error('Check-in unavailable'))
    const state = render()
    await flushPromises()
    await state.activity.checkin()
    expect(state.activity.loadError.value).toBe('Check-in unavailable')
    expect(state.activity.claiming.value).toBe(false)
    expect(mocks.fetchSelf).not.toHaveBeenCalled()
    state.wrapper.unmount()
  })
  it('does not read or claim a disabled feature', async () => {
    app.checkinEnabled = false
    const state = render()
    await flushPromises()
    await state.activity.checkin()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(mocks.post).not.toHaveBeenCalled()
    expect(state.activity.status.value).toBeNull()
    state.wrapper.unmount()
  })
})
