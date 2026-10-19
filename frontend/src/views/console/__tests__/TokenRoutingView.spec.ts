import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { reactive } from 'vue'
import { ApiError } from '@/api/types'
import type {
  TokenRoute,
  TokenRouteChannel,
  TokenRoutePreview,
} from '@/api/tokenRouting'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  channels: vi.fn(),
  save: vi.fn(),
  preview: vi.fn(),
  success: vi.fn(),
  push: vi.fn(),
}))
const route = reactive({ params: { id: '7' } })
const permission = reactive({ read: true, write: true })
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: mocks.push }),
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    hasPermission: (_resource: string, action: 'read' | 'write') =>
      permission[action],
  }),
}))
vi.mock('@/api/tokenRouting', () => ({ tokenRoutingApi: mocks }))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
import TokenRoutingView from '../TokenRoutingView.vue'

function config(id = 7): TokenRoute {
  return {
    token_id: id,
    mode: 'manual',
    version: 1,
    max_channel_ratio: null,
    entries: [{ channel_id: id, enabled: true, position: 0 }],
  }
}
function channel(id: number, eligible = true): TokenRouteChannel {
  return {
    id,
    name: `Channel ${id}`,
    type: 1,
    status: 1,
    models: ['gpt'],
    channel_ratio: '1',
    max_concurrency: 0,
    in_flight: 0,
    eligible,
  }
}
function previewResult(): TokenRoutePreview {
  return {
    mode: 'manual',
    version: 1,
    model: 'gpt',
    path: '/v1/chat/completions',
    entries: [
      {
        channel_id: 7,
        name: 'Saved preview channel',
        eligible: true,
        reason: 'eligible',
        channel_ratio: '1',
        in_flight: 0,
      },
    ],
  }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}
beforeEach(() => {
  vi.resetAllMocks()
  route.params.id = '7'
  permission.read = true
  permission.write = true
  mocks.get.mockImplementation(async (id: number) => config(id))
  mocks.channels.mockImplementation(async (id: number) => [
    channel(id),
    channel(id + 1),
    channel(id + 2, false),
  ])
  mocks.save.mockImplementation(async (_id: number, value: TokenRoute) => ({
    ...value,
    version: value.version + 1,
  }))
  mocks.preview.mockResolvedValue(previewResult())
})
async function render() {
  const wrapper = mount(TokenRoutingView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en' })],
      stubs: {
        PageBreadcrumb: { template: '<div><slot name="action" /></div>' },
      },
    },
  })
  await flushPromises()
  return wrapper
}
function button(wrapper: VueWrapper, label: string) {
  return wrapper
    .findAll('button')
    .find(
      (item) => item.text() === label || item.attributes('aria-label') === label
    )!
}
function ids(wrapper: VueWrapper) {
  return wrapper
    .findAll('[data-channel-id]')
    .map((item) => Number(item.attributes('data-channel-id')))
}
async function withTwoChannels() {
  mocks.get.mockResolvedValue({
    ...config(),
    entries: [
      { channel_id: 7, enabled: true, position: 0 },
      { channel_id: 8, enabled: true, position: 1 },
    ],
  })
  return render()
}

it('blocks another write after a conflict until authoritative reload succeeds', async () => {
  mocks.save.mockRejectedValueOnce(
    new ApiError('changed', { status: 409, code: 'ROUTING_VERSION_CONFLICT' })
  )
  const wrapper = await render()
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Reload before saving')
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  expect(wrapper.find('[data-channel-id]').attributes('draggable')).toBe(
    'false'
  )
  await button(wrapper, 'Reload').trigger('click')
  await flushPromises()
  expect(button(wrapper, 'Save').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
it('does not apply a pending save to another token after navigation', async () => {
  const pending = deferred<TokenRoute>()
  mocks.save.mockReturnValue(pending.promise)
  const wrapper = await render()
  await button(wrapper, 'Save').trigger('click')
  route.params.id = '8'
  await flushPromises()
  pending.resolve({ ...config(), version: 2 })
  await flushPromises()
  expect(wrapper.text()).toContain('Channel 8')
  expect(wrapper.find('[data-channel-id="7"]').exists()).toBe(false)
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('revoking routing write permission invalidates a pending mutation result', async () => {
  const pending = deferred<TokenRoute>()
  mocks.save.mockReturnValue(pending.promise)
  const wrapper = await render()
  await button(wrapper, 'Save').trigger('click')
  permission.write = false
  await flushPromises()
  pending.resolve({ ...config(), version: 2 })
  await flushPromises()
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  expect(mocks.success).not.toHaveBeenCalled()
  expect(wrapper.text()).toContain('cannot edit')
  wrapper.unmount()
})
it('moves channels with accessible arrow buttons and saves the visible order', async () => {
  const wrapper = await withTwoChannels()
  await wrapper
    .find('[data-channel-id="8"] button[aria-label="Move up"]')
    .trigger('click')
  expect(ids(wrapper)).toEqual([8, 7])
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  expect(mocks.save).toHaveBeenCalledWith(
    7,
    expect.objectContaining({
      mode: 'manual',
      entries: [
        { channel_id: 8, enabled: true, position: 0 },
        { channel_id: 7, enabled: true, position: 1 },
      ],
    })
  )
  expect(
    wrapper
      .find('[data-channel-id="8"] button[aria-label="Move up"]')
      .attributes('disabled')
  ).toBeDefined()
  wrapper.unmount()
})
it('reorders by drag and drop and ignores cancelled or foreign drags', async () => {
  const wrapper = await withTwoChannels()
  const transfer = { setData: vi.fn(), effectAllowed: '' }
  await wrapper
    .find('[data-channel-id="7"]')
    .trigger('dragstart', { dataTransfer: transfer })
  await wrapper.find('[data-channel-id="8"]').trigger('drop')
  expect(transfer.setData).toHaveBeenCalledWith('text/plain', '7')
  expect(ids(wrapper)).toEqual([8, 7])
  await wrapper.find('[data-channel-id="8"]').trigger('drop')
  expect(ids(wrapper)).toEqual([8, 7])
  await wrapper.find('[data-channel-id="7"]').trigger('dragstart')
  await wrapper.find('[data-channel-id="7"]').trigger('dragend')
  await wrapper.find('[data-channel-id="8"]').trigger('drop')
  expect(ids(wrapper)).toEqual([8, 7])
  wrapper.unmount()
})
it('adds eligible platform channels without duplicates and preserves disabled entries', async () => {
  const wrapper = await render()
  const select = wrapper.find('select[aria-label="Select a platform channel"]')
  expect(select.find('option[value="9"]').attributes('disabled')).toBeDefined()
  await select.setValue('8')
  await button(wrapper, 'Add channel').trigger('click')
  expect(ids(wrapper)).toEqual([7, 8])
  expect(select.find('option[value="8"]').exists()).toBe(false)
  await wrapper
    .find('[data-channel-id="8"] button[role="switch"]')
    .trigger('click')
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  expect(mocks.save.mock.calls[0]![1].entries[1]).toEqual({
    channel_id: 8,
    enabled: false,
    position: 1,
  })
  wrapper.unmount()
})
it('allows an explicitly empty manual list and explains that there is no automatic fallback', async () => {
  const wrapper = await render()
  await button(wrapper, 'Remove').trigger('click')
  expect(ids(wrapper)).toEqual([])
  expect(wrapper.text()).toContain('Manual mode does not fall back')
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  expect(mocks.save).toHaveBeenCalledWith(
    7,
    expect.objectContaining({ mode: 'manual', entries: [] })
  )
  wrapper.unmount()
})
it.each(['0', '1000.000000000000000001', '0.0000000000000000001', '-1', '1e2'])(
  'rejects an invalid channel multiplier %s before saving',
  async (ratio) => {
    const wrapper = await render()
    await wrapper
      .find('input[aria-label="Maximum channel multiplier"]')
      .setValue(ratio)
    await button(wrapper, 'Save').trigger('click')
    expect(mocks.save).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('at most 18 decimal places')
    wrapper.unmount()
  }
)
it('keeps read-only previews separate from unsaved edits and clears superseded results', async () => {
  const pending = deferred<TokenRoutePreview>()
  mocks.preview.mockReturnValueOnce(pending.promise)
  const wrapper = await render()
  await wrapper.find('input[aria-label="Model"]').setValue('gpt')
  await button(wrapper, 'Preview saved routing').trigger('click')
  await wrapper.find('input[aria-label="Model"]').setValue('other-model')
  pending.resolve(previewResult())
  await flushPromises()
  expect(wrapper.text()).not.toContain('Saved preview channel')
  await button(wrapper, 'Preview saved routing').trigger('click')
  await flushPromises()
  expect(mocks.preview).toHaveBeenLastCalledWith(
    7,
    'other-model',
    '/v1/chat/completions',
    expect.any(AbortSignal)
  )
  expect(wrapper.text()).toContain('Saved preview channel')
  expect(mocks.save).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('clears displayed routing and pending previews as soon as read permission is revoked', async () => {
  const pending = deferred<TokenRoutePreview>()
  mocks.preview.mockReturnValueOnce(pending.promise)
  const wrapper = await render()
  await wrapper.find('input[aria-label="Model"]').setValue('gpt')
  await button(wrapper, 'Preview saved routing').trigger('click')
  permission.read = false
  await flushPromises()
  pending.resolve(previewResult())
  await flushPromises()
  expect(wrapper.find('[data-channel-id]').exists()).toBe(false)
  expect(wrapper.text()).not.toContain('Saved preview channel')
  expect(wrapper.text()).toContain('no longer have permission')
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  wrapper.unmount()
})
it('does not leave old editable data visible after a failed reload', async () => {
  const wrapper = await render()
  mocks.get.mockRejectedValueOnce(new Error('Loading failed'))
  await button(wrapper, 'Reload').trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Loading failed')
  expect(wrapper.find('[data-channel-id]').exists()).toBe(false)
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  wrapper.unmount()
})
it.each([
  { initial: '1', saved: '2', before: false, after: true },
  { initial: '2', saved: '1', before: true, after: false },
])(
  'refreshes candidate eligibility after changing the multiplier limit from $initial to $saved',
  async ({ initial, saved, before, after }) => {
    const candidate = { ...channel(8, before), channel_ratio: '2' }
    mocks.get.mockResolvedValue({ ...config(), max_channel_ratio: initial })
    mocks.channels
      .mockResolvedValueOnce([channel(7), candidate])
      .mockResolvedValueOnce([
        channel(7),
        {
          ...candidate,
          eligible: after,
          filter_reason: after ? undefined : 'channel_ratio_exceeded',
        },
      ])
    const wrapper = await render()
    const option = () => wrapper.get('option[value="8"]')
    expect(option().attributes('disabled') !== undefined).toBe(!before)
    await wrapper
      .get('input[aria-label="Maximum channel multiplier"]')
      .setValue(saved)
    await button(wrapper, 'Save').trigger('click')
    await flushPromises()
    expect(mocks.channels).toHaveBeenLastCalledWith(7, expect.any(AbortSignal))
    expect(mocks.channels).toHaveBeenCalledTimes(2)
    expect(option().attributes('disabled') !== undefined).toBe(!after)
    expect(button(wrapper, 'Save').attributes('disabled')).toBeUndefined()
    await button(wrapper, 'Save').trigger('click')
    await flushPromises()
    expect(mocks.save.mock.calls[1]![1].version).toBe(2)
    wrapper.unmount()
  }
)
it('keeps the confirmed save when candidate refresh fails and requires a reload before editing', async () => {
  const wrapper = await render()
  mocks.channels.mockRejectedValueOnce(new Error('Candidate service failed'))
  await wrapper
    .get('input[aria-label="Maximum channel multiplier"]')
    .setValue('2')
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  expect(mocks.success).toHaveBeenCalledWith('Routing saved.')
  expect(wrapper.text()).toContain('Routing was saved, but channel candidates')
  expect(wrapper.text()).toContain('Candidate service failed')
  expect(
    (
      wrapper.get('input[aria-label="Maximum channel multiplier"]')
        .element as HTMLInputElement
    ).value
  ).toBe('2')
  expect(ids(wrapper)).toEqual([7])
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  expect(
    wrapper
      .get('select[aria-label="Select a platform channel"]')
      .attributes('disabled')
  ).toBeDefined()
  expect(mocks.save).toHaveBeenCalledTimes(1)
  mocks.get.mockResolvedValue({
    ...config(),
    version: 2,
    max_channel_ratio: '2',
  })
  await button(wrapper, 'Reload').trigger('click')
  await flushPromises()
  expect(wrapper.text()).not.toContain('Candidate service failed')
  expect(button(wrapper, 'Save').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
it('aborts the post-save candidate read and clears its data when read permission is revoked', async () => {
  const pending = deferred<TokenRouteChannel[]>()
  const wrapper = await render()
  mocks.channels.mockReturnValueOnce(pending.promise)
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  const signal = mocks.channels.mock.calls[1]![1] as AbortSignal
  expect(wrapper.text()).toContain('Refreshing channel candidates')
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  permission.read = false
  await flushPromises()
  expect(signal.aborted).toBe(true)
  pending.resolve([channel(99)])
  await flushPromises()
  expect(wrapper.text()).not.toContain('Channel 99')
  expect(wrapper.find('[data-channel-id]').exists()).toBe(false)
  expect(wrapper.text()).toContain('no longer have permission')
  wrapper.unmount()
})
it('keeps write controls disabled when permission is revoked during the post-save refresh', async () => {
  const pending = deferred<TokenRouteChannel[]>()
  const wrapper = await render()
  mocks.channels.mockReturnValueOnce(pending.promise)
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  permission.write = false
  await flushPromises()
  pending.resolve([channel(7), channel(8)])
  await flushPromises()
  expect(button(wrapper, 'Save').attributes('disabled')).toBeDefined()
  expect(wrapper.text()).toContain('cannot edit')
  expect(mocks.save).toHaveBeenCalledTimes(1)
  wrapper.unmount()
})
it('discards a post-save candidate refresh after navigating to another token', async () => {
  const pending = deferred<TokenRouteChannel[]>()
  const wrapper = await render()
  mocks.channels.mockReturnValueOnce(pending.promise)
  await button(wrapper, 'Save').trigger('click')
  await flushPromises()
  const signal = mocks.channels.mock.calls[1]![1] as AbortSignal
  route.params.id = '8'
  await flushPromises()
  expect(signal.aborted).toBe(true)
  pending.resolve([channel(99)])
  await flushPromises()
  expect(wrapper.text()).toContain('Channel 8')
  expect(wrapper.text()).not.toContain('Channel 99')
  expect(button(wrapper, 'Save').attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
