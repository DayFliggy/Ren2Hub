import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { reactive } from 'vue'
import { ApiError } from '@/api/types'
const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  channels: vi.fn(),
  save: vi.fn(),
  preview: vi.fn(),
  success: vi.fn(),
}))
const route = reactive({ params: { id: '7' } }),
  permission = reactive({ write: true })
vi.mock('vue-router', () => ({ useRoute: () => route }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ hasPermission: () => permission.write }),
}))
vi.mock('@/api/tokenRouting', () => ({ tokenRoutingApi: mocks }))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
import TokenRoutingView from '../TokenRoutingView.vue'
const config = (id = 7) => ({
  token_id: id,
  mode: 'manual',
  version: 1,
  entries: [{ channel_id: id, enabled: true, position: 0 }],
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}
beforeEach(() => {
  vi.clearAllMocks()
  route.params.id = '7'
  permission.write = true
  mocks.get.mockImplementation(async (id) => config(id))
  mocks.channels.mockImplementation(async (id) => [
    { id, name: `Channel ${id}`, type: 1, status: 1, models: ['gpt'] },
  ])
  mocks.save.mockResolvedValue({ ...config(), version: 2 })
})
async function render() {
  const wrapper = mount(TokenRoutingView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en' })],
      stubs: { PageBreadcrumb: true },
    },
  })
  await flushPromises()
  return wrapper
}
it('blocks another write after a conflict until authoritative reload succeeds', async () => {
  mocks.save.mockRejectedValueOnce(
    new ApiError('changed', { status: 409, code: 'ROUTING_VERSION_CONFLICT' })
  )
  const wrapper = await render(),
    button = () => wrapper.findAll('button').find((b) => b.text() === 'Save')!
  await button().trigger('click')
  await flushPromises()
  expect(wrapper.text()).toContain('Reload before saving')
  expect(button().attributes('disabled')).toBeDefined()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Reload')!
    .trigger('click')
  await flushPromises()
  expect(button().attributes('disabled')).toBeUndefined()
  wrapper.unmount()
})
it('does not apply a pending save to another token after navigation', async () => {
  const pending = deferred<ReturnType<typeof config>>()
  mocks.save.mockReturnValue(pending.promise)
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Save')!
    .trigger('click')
  route.params.id = '8'
  await flushPromises()
  pending.resolve({ ...config(), version: 2 })
  await flushPromises()
  expect(wrapper.text()).toContain('Channel 8')
  expect(wrapper.text()).not.toContain('Channel 7')
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('revoking routing write permission invalidates a pending mutation result', async () => {
  const pending = deferred<ReturnType<typeof config>>()
  mocks.save.mockReturnValue(pending.promise)
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'Save')!
    .trigger('click')
  permission.write = false
  await flushPromises()
  pending.resolve({ ...config(), version: 2 })
  await flushPromises()
  expect(
    wrapper
      .findAll('button')
      .find((b) => b.text() === 'Save')!
      .attributes('disabled')
  ).toBeDefined()
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})
