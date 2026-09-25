import { defineComponent, type PropType } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import TaskPluginsView from '../TaskPluginsView.vue'
import { taskPluginsApi } from '@/api/taskPlugins'
import type { PriceVersion } from '@/api/modelPricing'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const draft: PriceVersion = {
  id: 0,
  model: 'task-model',
  mode: 'expression',
  expression: 'v2:u("seconds")',
  prices: [],
  created_at: 0,
  hash: '',
}
const PricingStub = defineComponent({
  props: {
    seedDraft: { type: Object as PropType<PriceVersion>, required: true },
  },
  template: '<div data-price-draft>{{ seedDraft.expression }}</div>',
})
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.spyOn(taskPluginsApi, 'list').mockResolvedValue([
    {
      meta: {
        key: 'fixture',
        name: 'Fixture',
        version: '1',
        apiVersion: 1,
        models: ['task-model'],
      },
      source: 'custom',
      source_hash: 'hash',
      active: true,
      enabled: true,
      runtime_status: 'registered',
      channel_count: 0,
      in_flight_count: 0,
    },
  ])
  vi.spyOn(taskPluginsApi, 'runtime').mockResolvedValue({
    current_generation: 1,
    database_revision: '1',
    last_rebuild: { status: 'success' },
    plugin_errors: {},
  })
  vi.spyOn(taskPluginsApi, 'sources').mockResolvedValue([])
  vi.spyOn(taskPluginsApi, 'versions').mockResolvedValue([])
})
describe('task plugin price drafts', () => {
  it('requires deletion confirmation and displays a retained-version failure without removing it', async () => {
    const version = {
      key: 'fixture',
      version: '0.9',
      source: 'source',
      source_hash: 'hash',
      active: false,
      enabled: false,
      factory: false,
    }
    const versions = vi
      .mocked(taskPluginsApi.versions)
      .mockResolvedValue([version])
    const remove = vi
      .spyOn(taskPluginsApi, 'remove')
      .mockRejectedValueOnce(
        new Error('version is referenced by an in-flight task')
      )
      .mockResolvedValue(undefined)
    const confirm = vi
      .spyOn(window, 'confirm')
      .mockReturnValueOnce(false)
      .mockReturnValue(true)
    const wrapper = mount(TaskPluginsView, {
      global: { plugins: [i18n], stubs: { ModelPricingEditor: PricingStub } },
    })
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Versions and source')!
      .trigger('click')
    await flushPromises()
    const deleteButton = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Delete version')!
    await deleteButton.trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledOnce()
    expect(remove).not.toHaveBeenCalled()
    await deleteButton.trigger('click')
    await flushPromises()
    expect(remove).toHaveBeenCalledWith('fixture', '0.9')
    expect(wrapper.get('[role=alert]').text()).toContain('in-flight task')
    expect(wrapper.text()).toContain('0.9')
    versions.mockResolvedValue([])
    await deleteButton.trigger('click')
    await flushPromises()
    expect(wrapper.find('[role=alert]').exists()).toBe(false)
    expect(
      wrapper
        .findAll('button')
        .some((button) => button.text() === 'Delete version')
    ).toBe(false)
    wrapper.unmount()
  })
  it('does not restore a stale price draft after its expression changes', async () => {
    let resolve!: (value: { price_version: PriceVersion }) => void
    const pending = new Promise<{ price_version: PriceVersion }>((done) => {
      resolve = done
    })
    const priceDraft = vi
      .spyOn(taskPluginsApi, 'priceDraft')
      .mockReturnValueOnce(pending)
      .mockResolvedValue({ price_version: { ...draft, expression: 'v2:2' } })
    const wrapper = mount(TaskPluginsView, {
      global: { plugins: [i18n], stubs: { ModelPricingEditor: PricingStub } },
    })
    await flushPromises()
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Versions and source')!
      .trigger('click')
    await flushPromises()
    await wrapper.get('section select').setValue('task-model')
    await wrapper.get('section input').setValue('v2:1')
    const createDraft = wrapper
      .findAll('button')
      .find((b) => b.text() === 'Build pricing draft')!
    await createDraft.trigger('click')
    expect(priceDraft).toHaveBeenCalledWith('fixture', 'task-model', 'v2:1')
    await wrapper.get('section input').setValue('v2:2')
    resolve({ price_version: draft })
    await flushPromises()
    expect(wrapper.find('[data-price-draft]').exists()).toBe(false)
    await createDraft.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-price-draft]').text()).toBe('v2:2')
    wrapper.unmount()
  })
})
