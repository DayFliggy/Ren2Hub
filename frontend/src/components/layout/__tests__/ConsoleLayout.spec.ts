import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import ConsoleLayout from '../ConsoleLayout.vue'

const page = (name: string) =>
  defineComponent({ template: `<p data-page="${name}">${name}</p>` })
const Page = page('page')
const Keys = page('keys')
const Tasks = page('tasks')
const Audit = page('audit')

function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    // The layout is mounted directly, so its RouterView renders the pages.
    routes: [
      { path: '/logs', component: Page },
      { path: '/wallet', component: Page },
      { path: '/keys', component: Keys },
      { path: '/tasks', component: Tasks, meta: { tabGroup: 'logs' } },
      { path: '/audit', component: Audit, meta: { tabGroup: 'logs' } },
    ],
  })
  return router
}

function mountLayout(router: ReturnType<typeof setup>) {
  return mount(ConsoleLayout, {
    global: {
      plugins: [router],
      stubs: {
        ConsoleSidebar: true,
        ConsoleNavStrip: true,
        ConsoleTopbar: true,
      },
      mocks: { $t: (key: string) => key },
    },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(HTMLElement.prototype.scrollTo).mockClear()
})

it('starts each console page at the top of its scroll zone but keeps place on query updates', async () => {
  const router = setup()
  await router.push('/logs')
  const wrapper = mountLayout(router)
  await flushPromises()
  const zone = wrapper.get('[data-console-scroll-zone]').element
  const scrollTo = vi.mocked(HTMLElement.prototype.scrollTo)

  await router.push('/logs?page=2')
  await flushPromises()
  expect(scrollTo).not.toHaveBeenCalled()

  await router.push('/wallet')
  await flushPromises()
  expect(scrollTo).toHaveBeenCalledOnce()
  expect(scrollTo.mock.contexts[0]).toBe(zone)
  expect(scrollTo).toHaveBeenCalledWith({ top: 0 })
  wrapper.unmount()
})

it('mounts the next page at once instead of waiting for the previous page to leave', async () => {
  const router = setup()
  await router.push('/keys')
  const wrapper = mountLayout(router)
  await flushPromises()

  await router.push('/tasks')
  // No animation frame has run: a leave transition would still hold the page.
  await flushPromises()

  expect(wrapper.find('[data-page="tasks"]').exists()).toBe(true)
  const previous = wrapper.find('[data-page="keys"]')
  if (previous.exists())
    expect(previous.classes()).toContain('page-leave-active')
  wrapper.unmount()
})

it('marks a swap between tabs of one page so the shared header stays in place', async () => {
  const router = setup()
  await router.push('/keys')
  const wrapper = mountLayout(router)
  await flushPromises()
  const content = wrapper.get('.console-content')

  await router.push('/tasks')
  await flushPromises()
  expect(content.attributes('data-page-swap')).toBe('page')

  await router.push('/audit')
  await flushPromises()
  expect(content.attributes('data-page-swap')).toBe('tab')

  await router.push('/keys')
  await flushPromises()
  expect(content.attributes('data-page-swap')).toBe('page')
  wrapper.unmount()
})
