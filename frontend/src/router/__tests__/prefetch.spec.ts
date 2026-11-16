import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

const loadMessageDomain = vi.hoisted(() => vi.fn(() => Promise.resolve()))
vi.mock('@/i18n', () => ({ loadMessageDomain }))

import {
  installRoutePrefetch,
  prefetchRoute,
  prefetchRouteAfterArrival,
} from '@/router/prefetch'

const Page = defineComponent({ template: '<p>page</p>' })

function setup() {
  const loaders = {
    shell: vi.fn(() => Promise.resolve(Page)),
    keys: vi.fn(() => Promise.resolve(Page)),
    logs: vi.fn(() => Promise.resolve(Page)),
  }
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Page },
      {
        path: '/console',
        component: loaders.shell,
        meta: { messageDomain: 'console' },
        children: [
          { path: '/keys', name: 'keys', component: loaders.keys },
          { path: '/usage-logs', name: 'logs', component: loaders.logs },
        ],
      },
    ],
  })
  return { router, loaders }
}

function link(href: string, attributes: Record<string, string> = {}) {
  const anchor = document.createElement('a')
  anchor.href = href
  for (const [name, value] of Object.entries(attributes))
    anchor.setAttribute(name, value)
  const label = document.createElement('span')
  anchor.append(label)
  document.body.append(anchor)
  return label
}

function pointerOver(target: Element, pointerType = 'mouse') {
  target.dispatchEvent(
    Object.assign(new Event('pointerover', { bubbles: true }), { pointerType })
  )
}

/** Lets a pointer that stopped moving reach the hover dwell time. */
async function rest() {
  vi.advanceTimersByTime(80)
  await flushPromises()
}

let stop: (() => void) | undefined

beforeEach(() => {
  vi.useFakeTimers()
  loadMessageDomain.mockClear()
})

afterEach(() => {
  vi.useRealTimers()
  stop?.()
  stop = undefined
  document.body.replaceChildren()
  Reflect.deleteProperty(navigator, 'connection')
})

describe('route prefetch', () => {
  it('loads the page, its shell and its messages once', async () => {
    const { router, loaders } = setup()

    prefetchRoute(router, { name: 'keys' })
    prefetchRoute(router, '/keys?page=2')
    await flushPromises()

    expect(loaders.shell).toHaveBeenCalledOnce()
    expect(loaders.keys).toHaveBeenCalledOnce()
    expect(loaders.logs).not.toHaveBeenCalled()
    expect(loadMessageDomain).toHaveBeenCalledWith('console')
  })

  it('leaves a failed download to be retried by a later request', async () => {
    const { router, loaders } = setup()
    loaders.keys.mockRejectedValueOnce(new Error('offline'))

    prefetchRoute(router, { name: 'keys' })
    await flushPromises()
    prefetchRoute(router, { name: 'keys' })
    await flushPromises()

    expect(loaders.keys).toHaveBeenCalledTimes(2)
  })

  it('prefetches a likely next page only after the current page has arrived', async () => {
    const { router, loaders } = setup()

    prefetchRouteAfterArrival(router, { name: 'keys' })
    vi.advanceTimersByTime(300)
    expect(loaders.shell).not.toHaveBeenCalled()
    vi.advanceTimersByTime(100)
    await flushPromises()

    expect(loaders.keys).toHaveBeenCalledOnce()
  })

  it('prefetches when a mouse rests on an internal link', async () => {
    const { router, loaders } = setup()
    stop = installRoutePrefetch(router)

    pointerOver(link('/keys'))
    vi.advanceTimersByTime(40)
    expect(loaders.keys).not.toHaveBeenCalled()
    await rest()

    expect(loaders.keys).toHaveBeenCalledOnce()
  })

  it('skips the links a mouse only passes over', async () => {
    const { router, loaders } = setup()
    stop = installRoutePrefetch(router)
    const keys = link('/keys')
    const logs = link('/usage-logs')

    pointerOver(keys)
    vi.advanceTimersByTime(40)
    pointerOver(logs)
    vi.advanceTimersByTime(40)
    pointerOver(document.body)
    await rest()
    expect(loaders.shell).not.toHaveBeenCalled()

    pointerOver(keys)
    vi.advanceTimersByTime(40)
    pointerOver(logs)
    await rest()
    expect(loaders.keys).not.toHaveBeenCalled()
    expect(loaders.logs).toHaveBeenCalledOnce()
  })

  it('prefetches at once when a touch or the keyboard reaches a link', async () => {
    const { router, loaders } = setup()
    stop = installRoutePrefetch(router)

    pointerOver(link('/keys'), 'touch')
    link('/usage-logs').dispatchEvent(new Event('focusin', { bubbles: true }))

    expect(loaders.keys).toHaveBeenCalledOnce()
    expect(loaders.logs).toHaveBeenCalledOnce()
  })

  it('prefetches for a control that names its route', async () => {
    const { router, loaders } = setup()
    stop = installRoutePrefetch(router)
    const button = document.createElement('button')
    button.setAttribute('data-prefetch-route', 'logs')
    document.body.append(button)

    pointerOver(button)
    await rest()

    expect(loaders.logs).toHaveBeenCalledOnce()
  })

  it('ignores external links, new tabs, downloads and unknown routes', async () => {
    const { router, loaders } = setup()
    stop = installRoutePrefetch(router)
    const unknown = document.createElement('button')
    unknown.setAttribute('data-prefetch-route', 'missing')
    document.body.append(unknown)

    for (const target of [
      link('https://docs.example.com/keys'),
      link('/keys', { target: '_blank' }),
      link('/keys', { download: '' }),
      unknown,
    ]) {
      pointerOver(target)
      await rest()
    }

    expect(loaders.keys).not.toHaveBeenCalled()
    expect(loaders.shell).not.toHaveBeenCalled()
  })

  it('does not prefetch when the browser asks to save data', async () => {
    Object.defineProperty(navigator, 'connection', {
      configurable: true,
      value: { saveData: true },
    })
    const { router, loaders } = setup()
    stop = installRoutePrefetch(router)

    pointerOver(link('/keys'), 'touch')
    await rest()

    expect(loaders.keys).not.toHaveBeenCalled()
  })
})
