import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  fetchSelf: vi.fn(),
  push: vi.fn(),
  reset: vi.fn(),
  user: { aff_count: 3, aff_quota: 1000000, aff_history_quota: 1500000 },
  app: { checkinEnabled: false, turnstileEnabled: false, turnstileSiteKey: '' },
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.app }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks }))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mocks.push }),
}))
import ActivityView from '../ActivityView.vue'
const status = {
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
        ActivityHero: true,
        ActivityEntryCard: true,
        RouterLink: true,
        TurnstileWidget: defineComponent({
          name: 'TurnstileWidget',
          props: { siteKey: { type: String, default: '' } },
          emits: ['verified', 'unavailable'],
          setup(_props, { expose }) {
            expose({ reset: mocks.reset })
          },
          template: '<div data-turnstile />',
        }),
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
  await wrapper.get('[data-checkin-action]').trigger('click')
  await flushPromises()
  expect(mocks.post).toHaveBeenCalledWith('/api/user/checkin', undefined, {
    params: {},
  })
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
  expect(wrapper.find('[data-checkin-action]').exists()).toBe(false)
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
  // Invite totals come from the account, not the failed check-in request.
  expect(wrapper.text()).toMatch(/Invited users\s*3/)
  wrapper.unmount()
})

it('keeps invite totals visible while the check-in status is still loading', async () => {
  mocks.app.checkinEnabled = true
  mocks.get.mockReturnValue(new Promise(() => {}))
  const wrapper = setup()
  await flushPromises()
  expect(wrapper.find('[data-checkin-card]').exists()).toBe(false)
  expect(wrapper.text()).toMatch(/Invited users\s*3/)
  wrapper.unmount()
})

it('does not post a claim when the card requests one for an already claimed day', async () => {
  mocks.get.mockResolvedValue({
    ...status,
    stats: { ...status.stats, checked_in_today: true },
  })
  mocks.app.checkinEnabled = true
  const wrapper = setup()
  await flushPromises()
  wrapper.findComponent({ name: 'CheckinCard' }).vm.$emit('checkin')
  await flushPromises()
  expect(mocks.post).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('uses real random reward bounds, calendar records and separate cumulative totals', async () => {
  mocks.app.checkinEnabled = true
  mocks.get.mockResolvedValue({
    ...status,
    min_quota: 500000,
    max_quota: 2500000,
    today: '2026-10-09',
    stats: {
      ...status.stats,
      total_checkins: 10,
      total_quota: 10000000,
      checkin_count: 2,
      records: [
        { checkin_date: '2026-10-05', quota_awarded: 500000 },
        { checkin_date: '2026-10-09', quota_awarded: 1500000 },
      ],
    },
  })
  const wrapper = setup()
  await flushPromises()
  await wrapper.get('input[type="month"]').setValue('2026-10')
  await flushPromises()
  const card = wrapper.get('[data-checkin-card]')
  expect(card.text()).toContain('$1.00 – $5.00')
  expect(card.text()).toContain('Selected month rewards $4.00')
  expect(card.find('.pencil-progress').exists()).toBe(false)
  expect(card.text()).toMatch(/Total check-ins\s*10/)
  expect(card.text()).toMatch(/Total rewards\s*\$20\.00/)
  expect(card.findAll('[data-checkin-record]')).toHaveLength(2)
  expect(card.find('[data-checkin-record="2026-10-05"]').exists()).toBe(true)
  expect(card.text()).not.toMatch(/streak|WEEK|Day 7|next reward/i)
  wrapper.unmount()
})

it('restores the invitation card with native totals and opens the existing invite page', async () => {
  const wrapper = setup()
  await flushPromises()
  expect(wrapper.text()).toContain('Account totals')
  expect(wrapper.text()).toMatch(/Invited users\s*3/)
  expect(wrapper.text()).toMatch(/Available referral quota\s*\$2\.00/)
  expect(wrapper.text()).toMatch(/Lifetime referral quota\s*\$3\.00/)
  const action = wrapper
    .findAll('button')
    .find((button) => button.text() === 'View invitation and transfer rewards')!
  await action.trigger('click')
  expect(mocks.push).toHaveBeenCalledWith({ name: 'invite' })
  expect(mocks.get).not.toHaveBeenCalled()
  expect(mocks.post).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('requires Turnstile verification and clears it after a native check-in', async () => {
  mocks.app.checkinEnabled = true
  mocks.app.turnstileEnabled = true
  mocks.app.turnstileSiteKey = 'test-site-key'
  const wrapper = setup()
  await flushPromises()
  expect(wrapper.get('[data-checkin-action]').attributes('disabled')).toBe('')
  const turnstile = wrapper.findComponent({ name: 'TurnstileWidget' })
  turnstile.vm.$emit('verified', 'verified-token')
  await flushPromises()
  expect(
    wrapper.get('[data-checkin-action]').attributes('disabled')
  ).toBeUndefined()
  await wrapper.get('[data-checkin-action]').trigger('click')
  await flushPromises()
  expect(mocks.post).toHaveBeenCalledWith('/api/user/checkin', undefined, {
    params: { turnstile: 'verified-token' },
  })
  expect(wrapper.get('[data-checkin-action]').attributes('disabled')).toBe('')
  wrapper.unmount()
})

it('blocks claims when the Turnstile challenge is unavailable or expires', async () => {
  mocks.app.checkinEnabled = true
  mocks.app.turnstileEnabled = true
  mocks.app.turnstileSiteKey = 'test-site-key'
  const wrapper = setup()
  await flushPromises()
  const turnstile = wrapper.findComponent({ name: 'TurnstileWidget' })
  turnstile.vm.$emit('verified', 'verified-token')
  turnstile.vm.$emit('verified', '')
  await flushPromises()
  expect(wrapper.get('[data-checkin-action]').attributes('disabled')).toBe('')
  turnstile.vm.$emit('verified', 'verified-token')
  turnstile.vm.$emit('unavailable')
  await flushPromises()
  await wrapper.get('[data-checkin-action]').trigger('click')
  expect(mocks.post).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('recovers the claim after a failed Turnstile challenge is solved again', async () => {
  mocks.app.checkinEnabled = true
  mocks.app.turnstileEnabled = true
  mocks.app.turnstileSiteKey = 'test-site-key'
  const wrapper = setup()
  await flushPromises()
  expect(mocks.get).toHaveBeenCalledOnce()
  const turnstile = wrapper.findComponent({ name: 'TurnstileWidget' })
  turnstile.vm.$emit('unavailable')
  turnstile.vm.$emit('verified', 'retried-token')
  await flushPromises()
  await wrapper.get('[data-checkin-action]').trigger('click')
  await flushPromises()
  expect(mocks.post).toHaveBeenCalledWith('/api/user/checkin', undefined, {
    params: { turnstile: 'retried-token' },
  })
  wrapper.unmount()
})

it('does not mount a Turnstile challenge once today is already claimed', async () => {
  mocks.get.mockResolvedValue({
    ...status,
    stats: { ...status.stats, checked_in_today: true },
  })
  mocks.app.checkinEnabled = true
  mocks.app.turnstileEnabled = true
  mocks.app.turnstileSiteKey = 'test-site-key'
  const wrapper = setup()
  await flushPromises()
  expect(wrapper.findComponent({ name: 'TurnstileWidget' }).exists()).toBe(
    false
  )
  wrapper.unmount()
})
