import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useToast } from '@/composables/useToast'
import PluginEndpoints from '../components/PluginEndpoints.vue'
import { usePluginChangeNotice } from '../usePluginChangeNotice'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

describe('native plugin routes and pricing notices', () => {
  it('marks unregistered vendor routes and copies their reachable key address', async () => {
    const clipboard = { writeText: vi.fn().mockResolvedValue(undefined) }
    vi.stubGlobal('navigator', { ...navigator, clipboard })
    const wrapper = mount(PluginEndpoints, {
      props: {
        pluginKey: 'alibaba',
        routes: [
          { method: 'POST', path: '/api/image', type: 'submit' },
          { method: 'POST', path: '/ali/image', type: 'submit' },
        ],
        skippedRoutes: ['POST /api/image'],
      },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('Only served at the /alibaba address')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === '/alibaba')!
      .trigger('click')
    expect(wrapper.text()).toContain('/alibaba/api/image')
    expect(wrapper.text()).toContain('/ali/image')
    expect(wrapper.text()).not.toContain('/alibaba/ali/image')
    expect(wrapper.text()).not.toContain('Only served')
    await wrapper
      .get('button[aria-label="Copy endpoint path"]')
      .trigger('click')
    await flushPromises()
    expect(clipboard.writeText).toHaveBeenCalledWith('/alibaba/api/image')
    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('reports a saved change with all affected providers instead of plain success', () => {
    let notify!: ReturnType<typeof usePluginChangeNotice>
    const wrapper = mount(
      {
        setup() {
          notify = usePluginChangeNotice()
          return {}
        },
        template: '<div />',
      },
      { global: { plugins: [i18n] } }
    )
    const toast = useToast()
    notify('Plugin version activated', [
      {
        model_name: 'shared-model',
        plugin_key: 'first',
        plugin_name: 'First',
        reason: 'unconfigured',
      },
      {
        model_name: 'shared-model',
        plugin_key: 'second',
        plugin_name: 'Second',
        reason: 'incompatible',
      },
    ])
    const warning = toast.toasts.at(-1)!
    expect(warning.type).toBe('warning')
    expect(warning.message).toContain('Plugin version activated')
    expect(warning.message).toContain('shared-model (First): No price set')
    expect(warning.message).toContain(
      'shared-model (Second): The price uses usage'
    )
    toast.dismiss(warning.id)
    notify('Plugin version activated', null)
    const success = toast.toasts.at(-1)!
    expect(success.type).toBe('success')
    toast.dismiss(success.id)
    wrapper.unmount()
  })
})
