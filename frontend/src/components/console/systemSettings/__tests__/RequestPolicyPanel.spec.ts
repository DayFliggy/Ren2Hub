import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
const api = vi.hoisted(() => ({ get: vi.fn(), patch: vi.fn() }))
vi.mock('@/api/client', () => ({ api }))
import RequestPolicyPanel from '../RequestPolicyPanel.vue'
beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockResolvedValue({
    options: { RetryTimes: '2', CheckSensitiveEnabled: 'false' },
  })
  api.patch.mockImplementation(async (_, body) => body)
})
async function render() {
  const wrapper = mount(RequestPolicyPanel, {
    global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
  })
  await flushPromises()
  return wrapper
}
it('sends one atomic native policy patch with string option values', async () => {
  const wrapper = await render()
  await wrapper.get('textarea').setValue('{"RetryTimes":"4"}')
  await wrapper.findAll('button')[0]!.trigger('click')
  await flushPromises()
  expect(api.patch).toHaveBeenCalledWith(
    '/api/option/request_policy',
    { options: { RetryTimes: '4' } },
    { signal: expect.any(AbortSignal) }
  )
  wrapper.unmount()
})
it.each(['{"RetryTimes":4}', '[]', 'invalid'])(
  'rejects invalid draft %s without a write',
  async (value) => {
    const wrapper = await render()
    await wrapper.get('textarea').setValue(value)
    await wrapper.findAll('button')[0]!.trigger('click')
    await flushPromises()
    expect(api.patch).not.toHaveBeenCalled()
    expect(wrapper.find('[role=alert]').exists()).toBe(true)
    wrapper.unmount()
  }
)
it('preserves draft and server rejection instead of claiming a successful save', async () => {
  const wrapper = await render()
  api.patch.mockRejectedValue(new Error('RetryTimes is invalid'))
  await wrapper.get('textarea').setValue('{"RetryTimes":"wrong"}')
  await wrapper.findAll('button')[0]!.trigger('click')
  await flushPromises()
  expect(wrapper.get('textarea').element.value).toContain('wrong')
  expect(wrapper.text()).toContain('RetryTimes is invalid')
  wrapper.unmount()
})
