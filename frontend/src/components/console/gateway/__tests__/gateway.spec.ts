import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { ApiError } from '@/api/types'
import GatewayConfigPanel from '../GatewayConfigPanel.vue'
import GatewayUserPanel from '../GatewayUserPanel.vue'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'

const mocks = vi.hoisted(() => ({
  config: vi.fn(),
  saveConfig: vi.fn(),
  admission: vi.fn(),
  userRuntime: vi.fn(),
  saveUser: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))
vi.mock('@/api/gateway', async (original) => ({
  ...(await original<object>()),
  gatewayApi: mocks,
}))
vi.mock('@/api/console', () => ({ api: { post: mocks.post, put: mocks.put } }))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

const config = {
  version: 7,
  user_concurrency: 10,
  wait_ms: 5000,
  user_queue_size: 20,
  channel_queue_size: 20,
  groups: { team: { concurrency: 3, wait_ms: null, queue_size: null } },
  channel_rates: { '12': { rpm: 8, tpm: 1000 } },
  credential_rates: {},
}
const admission = {
  user_id: 1,
  group: 'team',
  config_version: 7,
  override: { concurrency: null, wait_ms: null, queue_size: null },
  effective: {
    concurrency: 3,
    wait_ms: 5000,
    queue_size: 20,
    channel_queue_size: 20,
  },
  sources: {
    concurrency: 'group',
    wait_ms: 'system',
    queue_size: 'system',
    channel_queue_size: 'system',
  },
}
beforeAll(async () => {
  await setLocale('zh-CN')
  await loadMessageDomain('console')
})
beforeEach(() => {
  vi.resetAllMocks()
  mocks.config.mockResolvedValue(structuredClone(config))
  mocks.admission.mockResolvedValue(structuredClone(admission))
  mocks.userRuntime.mockResolvedValue({ in_flight: 2, queued: 1 })
  mocks.saveUser.mockResolvedValue({
    concurrency: 0,
    wait_ms: null,
    queue_size: null,
  })
})

describe('gateway controls', () => {
  it('preserves unrelated maps and prevents resaving a conflicting version', async () => {
    mocks.saveConfig.mockRejectedValue(
      new ApiError('conflict', { status: 409 })
    )
    const wrapper = mount(GatewayConfigPanel, { global: { plugins: [i18n] } })
    await flushPromises()
    await wrapper.find('input[type="number"]').setValue('0')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveConfig.mock.calls[0]?.[0]).toEqual({
      ...config,
      user_concurrency: 0,
    })
    expect(wrapper.find('[role="alert"]').text()).toContain('配置已变更')
    expect(
      wrapper.find('button[type="submit"]').attributes('disabled')
    ).toBeDefined()
    wrapper.unmount()
  })

  it('saves explicit zero user overrides while keeping inheritance', async () => {
    const wrapper = mount(GatewayUserPanel, {
      props: { userId: 1, editable: true },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('分组覆盖')
    await wrapper.find('input[type="checkbox"]').setValue(false)
    const save = wrapper
      .findAll('button')
      .find((button) => button.text().includes('保存限制'))!
    await save.trigger('click')
    await flushPromises()
    expect(mocks.saveUser).toHaveBeenCalledWith(
      1,
      { concurrency: 0, wait_ms: null, queue_size: null },
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })

  it('aborts stale user loads and does not replace a newer user', async () => {
    let finish: (value: unknown) => void = () => undefined
    mocks.admission.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = mount(GatewayUserPanel, {
      props: { userId: 1 },
      global: { plugins: [i18n] },
    })
    const previousSignal = mocks.admission.mock.calls[0]?.[1] as AbortSignal
    mocks.admission.mockResolvedValue({
      ...admission,
      user_id: 2,
      effective: { ...admission.effective, concurrency: 22 },
    })
    await wrapper.setProps({ userId: 2 })
    await flushPromises()
    expect(previousSignal.aborted).toBe(true)
    finish(admission)
    await flushPromises()
    expect(wrapper.text()).toContain('22')
    expect(wrapper.findAll('input')).toHaveLength(0)
    wrapper.unmount()
  })

  it('keeps configured limits visible when runtime is unavailable', async () => {
    mocks.userRuntime.mockRejectedValue(
      new ApiError('unavailable', { status: 503 })
    )
    const wrapper = mount(GatewayUserPanel, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(wrapper.text()).toContain('运行状态暂不可用')
    expect(wrapper.text()).toContain('分组覆盖')
    expect(mocks.admission).toHaveBeenCalledWith(
      undefined,
      expect.any(AbortSignal)
    )
    expect(mocks.saveUser).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('includes the selected conversion policy when creating a token', async () => {
    mocks.post.mockResolvedValue(undefined)
    const wrapper = mount(KeyFormModal, {
      props: { open: true, editing: null, models: [] },
      global: {
        plugins: [i18n],
        stubs: {
          GatewayUserPanel: true,
          ConsoleModal: {
            template: '<div><slot /><slot name="footer" /></div>',
          },
        },
      },
    })
    await wrapper.find('[name="token-name"]').setValue('gateway-token')
    await wrapper.find('[name="conversion-policy"]').setValue('strict')
    await wrapper
      .findAll('button')
      .find(
        (button) => button.text().trim() === i18n.global.t('common.confirm')
      )!
      .trigger('click')
    await flushPromises()
    expect(mocks.post).toHaveBeenCalledWith(
      '/api/token/',
      expect.objectContaining({
        conversion_policy: 'strict',
        name: 'gateway-token',
        unlimited_quota: true,
      })
    )
    wrapper.unmount()
  })
})
