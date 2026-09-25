import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import AdvancedChannelEditor from '../AdvancedChannelEditor.vue'

function setup(config: Record<string, unknown>) {
  const wrapper = mount(AdvancedChannelEditor, {
    props: {
      modelValue: JSON.stringify(config),
      'onUpdate:modelValue': (value: string) =>
        wrapper.setProps({ modelValue: value }),
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  return wrapper
}

describe('advanced route editing', () => {
  it('keeps incomplete routes editable and preserves unrelated configuration', async () => {
    const wrapper = setup({
      extension: { keep: true },
      advanced_routes: [
        {
          incoming_path: '/v1/responses',
          upstream_path: '/v1/responses',
          converter: 'none',
        },
      ],
    })
    await wrapper.findAll('input')[0]!.setValue('')
    await flushPromises()
    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    await wrapper.findAll('input')[0]!.setValue('/v1/responses')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    const config = JSON.parse(wrapper.props('modelValue'))
    expect(config.extension).toEqual({ keep: true })
    wrapper.unmount()
  })

  it('can add a route after removing the last route without discarding metadata', async () => {
    const wrapper = setup({
      extension: 'retained',
      advanced_routes: [
        {
          incoming_path: '/v1/responses',
          upstream_path: '/v1/responses',
          converter: 'none',
        },
      ],
    })
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'channels.removeRoute')!
      .trigger('click')
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'channels.addRoute')!
      .trigger('click')
    await flushPromises()
    const config = JSON.parse(wrapper.props('modelValue'))
    expect(config.extension).toBe('retained')
    expect(config.advanced_routes).toHaveLength(1)
    expect(config.advanced_routes[0].auth).toEqual({
      type: 'header',
      name: 'Authorization',
      value: 'Bearer {api_key}',
    })
    wrapper.unmount()
  })
})
