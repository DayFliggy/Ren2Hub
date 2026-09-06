import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'

const route = vi.hoisted(() => ({ query: {} as Record<string, string> }))
const replace = vi.hoisted(() => vi.fn())
const confirmPasswordReset = vi.hoisted(() => vi.fn())
vi.mock('vue-router', async () => ({
  ...(await vi.importActual<typeof import('vue-router')>('vue-router')),
  useRoute: () => route,
  useRouter: () => ({ replace }),
}))
vi.mock('@/api/auth', () => ({ authApi: { confirmPasswordReset } }))
import i18n, { loadMessageDomain } from '@/i18n'
import ResetPasswordConfirmView from '../ResetPasswordConfirmView.vue'

beforeEach(async () => {
  vi.clearAllMocks()
  route.query = { email: 'reset@example.com', token: 'example-code' }
  await loadMessageDomain('auth')
})

function render() {
  return mount(ResetPasswordConfirmView, {
    global: {
      plugins: [i18n],
      stubs: {
        AuthLayout: { template: '<div><slot /></div>' },
        RouterLink: true,
      },
    },
  })
}

it('requires confirmation, consumes a reset link once and removes its token from history', async () => {
  confirmPasswordReset.mockResolvedValue('example-generated-password')
  const wrapper = render()
  expect(confirmPasswordReset).not.toHaveBeenCalled()
  await wrapper.get('button').trigger('click')
  await flushPromises()
  expect(confirmPasswordReset).toHaveBeenCalledOnce()
  expect(replace).toHaveBeenCalledWith({ name: 'reset-confirm' })
  expect(wrapper.get('input').element.value).toBe('example-generated-password')
})

it('disables confirmation for incomplete reset links', () => {
  route.query = {}
  const wrapper = render()
  expect(
    wrapper
      .findAll('button')
      .some((button) => button.attributes('disabled') !== undefined)
  ).toBe(true)
  expect(confirmPasswordReset).not.toHaveBeenCalled()
})
