import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  fetchSelf: vi.fn(),
  app: { checkinEnabled: false, turnstileEnabled: false, turnstileSiteKey: '' },
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks }))
import ActivityView from '../ActivityView.vue'
const status = {
  enabled: true,
  min_quota: 100,
  max_quota: 500,
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
  mocks.app = reactive({
    checkinEnabled: false,
    turnstileEnabled: false,
    turnstileSiteKey: '',
  })
  mocks.get.mockResolvedValue(status)
  mocks.post.mockResolvedValue(null)
  mocks.fetchSelf.mockResolvedValue(null)
})
function setup() {
  return mount(ActivityView, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
      stubs: {
        PageHero: true,
        TurnstileWidget: true,
        ErrorBanner: {
          props: ['message'],
          template: '<p role="alert">{{message}}</p>',
        },
      },
    },
  })
}
it('shows disabled status without requesting check-in or exposing an action', async () => {
  const wrapper = setup()
  await flushPromises()
  expect(wrapper.get('[role=status]').text()).toBe(
    'Daily check-in is not enabled on this site.'
  )
  expect(mocks.get).not.toHaveBeenCalled()
  expect(mocks.post).not.toHaveBeenCalled()
  expect(wrapper.find('[role=alert]').exists()).toBe(false)
  expect(wrapper.find('input[type=month]').exists()).toBe(false)
  wrapper.unmount()
})
it('loads the native status when enabled and preserves the reward action', async () => {
  const wrapper = setup()
  mocks.app.checkinEnabled = true
  await flushPromises()
  expect(mocks.get).toHaveBeenCalledWith(
    '/api/user/checkin',
    { month: expect.stringMatching(/^\d{4}-\d{2}$/) },
    { signal: expect.any(AbortSignal) }
  )
  await wrapper.get('button').trigger('click')
  await flushPromises()
  expect(mocks.post).toHaveBeenCalledWith(
    '/api/user/checkin',
    undefined,
    undefined
  )
  expect(mocks.fetchSelf).toHaveBeenCalledOnce()
  wrapper.unmount()
})
it('cancels an old status response after the site disables check-in', async () => {
  let finish!: (value: typeof status) => void
  mocks.get.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      })
  )
  mocks.app.checkinEnabled = true
  const wrapper = setup()
  await flushPromises()
  const signal = mocks.get.mock.calls[0]![2].signal as AbortSignal
  mocks.app.checkinEnabled = false
  await flushPromises()
  expect(signal.aborted).toBe(true)
  finish(status)
  await flushPromises()
  expect(wrapper.text()).toContain(
    'Daily check-in is not enabled on this site.'
  )
  expect(wrapper.find('button').exists()).toBe(false)
  wrapper.unmount()
})
it('keeps real enabled-feature request failures visible', async () => {
  mocks.app.checkinEnabled = true
  mocks.get.mockRejectedValue(new Error('Check-in storage unavailable'))
  const wrapper = setup()
  await flushPromises()
  expect(wrapper.get('[role=alert]').text()).toBe(
    'Check-in storage unavailable'
  )
  wrapper.unmount()
})
