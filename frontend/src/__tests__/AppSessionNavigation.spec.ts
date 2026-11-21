import { createPinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, nextTick, onUnmounted } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '@/App.vue'
import { useAuthStore } from '@/stores/auth'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'

const safeReturn = vi.hoisted(() =>
  vi.fn((value: unknown) => (typeof value === 'string' ? value : null))
)
vi.mock('@/router', () => ({ sanitizeRedirect: safeReturn }))
vi.mock('@/composables/useTheme', () => ({ useTheme: () => undefined }))
vi.mock('@/composables/useAccountLanguage', () => ({
  useAccountLanguageAdoption: () => undefined,
}))

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  safeReturn.mockClear()
})

async function setup(path = '/private?filter=active#details') {
  const unmounted = vi.fn()
  const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
  const privatePage = defineComponent({
    setup() {
      useUnsavedChanges({
        dirty: () => true,
        busy: () => false,
        message: () => 'Synthetic unsaved draft',
      })
      onUnmounted(unmounted)
      return () => h('p', { 'data-sensitive': '' }, 'Sensitive account content')
    },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/private',
        component: privatePage,
        meta: { requiresAuth: true },
      },
      {
        path: '/public',
        component: { render: () => h('p', 'Public document') },
      },
      {
        path: '/sign-in',
        name: 'sign-in',
        component: { render: () => h('p', 'Sign in') },
      },
    ],
  })
  const pinia = createPinia()
  const auth = useAuthStore(pinia)
  const user = {
    id: 1,
    username: 'member',
    display_name: 'Member',
    email: 'member@example.test',
    role: 1,
    status: 1,
    quota: 100,
    used_quota: 0,
    request_count: 0,
  }
  auth.persist(user)
  auth.checked = true
  await router.push(path)
  wrapper = mount(App, {
    global: {
      plugins: [pinia, router],
      stubs: {
        AppErrorBoundary: { template: '<slot />' },
        NavigationErrorState: true,
        ToastHost: true,
        VersionUpdateNotice: true,
      },
    },
  })
  await flushPromises()
  return { auth, user, router, unmounted, confirm }
}

describe('root session navigation', () => {
  it('unmounts protected content before redirect, without letting a dirty draft block logout', async () => {
    const { auth, router, unmounted, confirm } = await setup()
    expect(wrapper!.find('[data-sensitive]').exists()).toBe(true)
    auth.invalidateLocalSession()
    await nextTick()
    expect(wrapper!.find('[data-sensitive]').exists()).toBe(false)
    await flushPromises()
    expect(unmounted).toHaveBeenCalledOnce()
    expect(confirm).not.toHaveBeenCalled()
    expect(safeReturn).toHaveBeenCalledWith('/private?filter=active#details')
    expect(router.currentRoute.value.name).toBe('sign-in')
    expect(router.currentRoute.value.query.redirect).toBe(
      '/private?filter=active#details'
    )
  })

  it('does not navigate while peer-session refresh is pending or after it succeeds', async () => {
    const { auth, user, router } = await setup()
    auth.persist(null)
    auth.checked = false
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/private')
    expect(safeReturn).not.toHaveBeenCalled()
    auth.persist(user)
    auth.checked = true
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/private')
    expect(wrapper!.find('[data-sensitive]').exists()).toBe(true)
  })

  it('redirects when a pending peer-session refresh confirms anonymous state', async () => {
    const { auth, router, confirm } = await setup()
    auth.persist(null)
    auth.checked = false
    await flushPromises()
    auth.checked = true
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('sign-in')
    expect(confirm).not.toHaveBeenCalled()
  })

  it('keeps public content visible when the session expires', async () => {
    const { auth, router } = await setup('/public?source=document')
    auth.invalidateLocalSession()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/public?source=document')
    expect(wrapper!.text()).toContain('Public document')
    expect(safeReturn).not.toHaveBeenCalled()
  })

  it('does not redirect a session restored before the next render', async () => {
    const { auth, user, router } = await setup()
    auth.invalidateLocalSession()
    auth.persist(user)
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/private')
    expect(safeReturn).not.toHaveBeenCalled()
  })

  it('coexists with the explicit sign-in navigation after ordinary logout', async () => {
    const { auth, router, confirm } = await setup()
    auth.invalidateLocalSession()
    await nextTick()
    await router.push({ name: 'sign-in' })
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('sign-in')
    expect(wrapper!.find('[data-sensitive]').exists()).toBe(false)
    expect(confirm).not.toHaveBeenCalled()
    expect(safeReturn.mock.calls.length).toBeLessThanOrEqual(1)
  })
})
