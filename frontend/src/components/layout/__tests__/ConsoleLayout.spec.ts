import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import ConsoleLayout from '../ConsoleLayout.vue'

const Page = defineComponent({ template: '<p>page</p>' })

function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    // The layout is mounted directly, so its RouterView renders the pages.
    routes: [
      { path: '/logs', component: Page },
      { path: '/wallet', component: Page },
    ],
  })
  return router
}

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(HTMLElement.prototype.scrollTo).mockClear()
})

it('starts each console page at the top of its scroll zone but keeps place on query updates', async () => {
  const router = setup()
  await router.push('/logs')
  const wrapper = mount(ConsoleLayout, {
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
