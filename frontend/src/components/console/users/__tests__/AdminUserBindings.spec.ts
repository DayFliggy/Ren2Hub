import { flushPromises, mount } from '@vue/test-utils'
import { computed } from 'vue'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import AdminUserBindings from '../AdminUserBindings.vue'
import { adminUserBindingsApi } from '@/api/adminUserBindings'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { AdminUser } from '@/types/console'
vi.mock('@/components/auth/SecurityProofDialog.vue', () => ({
  default: {
    setup(_: unknown, { expose }: { expose: (value: unknown) => void }) {
      expose({ request: vi.fn().mockResolvedValue('one-use-proof') })
      return () => null
    },
  },
}))
const state = vi.hoisted(() => ({
  isAdmin: true,
  isRoot: false,
  readOnly: false,
  user: { id: 10 },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state }))
vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: computed(() => state.readOnly) }),
}))
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.restoreAllMocks()
  state.isAdmin = true
  state.isRoot = false
  state.readOnly = false
})
function setup(id = 42, role = 1) {
  return mount(AdminUserBindings, {
    props: { user: { id, role, username: 'fixture' } as AdminUser },
    global: {
      plugins: [i18n],
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot/><slot name="footer"/></div>',
        },
      },
    },
  })
}
describe('admin identity unbinding', () => {
  it('blocks user, self, peer and disabled-feature requests', async () => {
    const read = vi.spyOn(adminUserBindingsApi, 'list')
    for (const scenario of [
      { id: 10, role: 1, admin: true, readOnly: false },
      { id: 42, role: 10, admin: true, readOnly: false },
      { id: 42, role: 1, admin: false, readOnly: false },
      { id: 42, role: 1, admin: true, readOnly: true },
    ]) {
      state.isAdmin = scenario.admin
      state.readOnly = scenario.readOnly
      const wrapper = setup(scenario.id, scenario.role)
      expect(wrapper.find('[data-admin-bindings]').exists()).toBe(false)
      wrapper.unmount()
    }
    expect(read).not.toHaveBeenCalled()
  })
  it('confirms deletion, keeps failures visible and reloads successful changes', async () => {
    const binding = {
      key: 'github',
      name: 'GitHub',
      path: '/api/user/42/bindings/github',
    }
    const read = vi
      .spyOn(adminUserBindingsApi, 'list')
      .mockResolvedValue([binding])
    const remove = vi
      .spyOn(adminUserBindingsApi, 'remove')
      .mockRejectedValueOnce(new Error('still bound'))
      .mockResolvedValue(undefined)
    const wrapper = setup()
    await wrapper.get('[data-admin-bindings]').trigger('click')
    await flushPromises()
    await wrapper.get('button[aria-label="Unbind GitHub"]').trigger('click')
    expect(remove).not.toHaveBeenCalled()
    await wrapper.findComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('still bound')
    expect(wrapper.findComponent({ name: 'ConfirmDialog' }).props('open')).toBe(
      true
    )
    read.mockResolvedValue([])
    await wrapper.findComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
    await flushPromises()
    expect(remove).toHaveBeenLastCalledWith(binding, 'one-use-proof')
    expect(wrapper.emitted('removed')).toEqual([['github']])
    expect(wrapper.text()).toContain('No bound sign-in')
    wrapper.unmount()
  })
  it('aborts profile reads when closed', async () => {
    const read = vi
      .spyOn(adminUserBindingsApi, 'list')
      .mockImplementation(() => new Promise(() => {}))
    const wrapper = setup()
    await wrapper.get('[data-admin-bindings]').trigger('click')
    const signal = read.mock.calls[0]![1]!
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})
