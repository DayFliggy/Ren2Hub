import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import TaskPluginsView from '../TaskPluginsView.vue'
import { taskPluginsApi } from '@/api/taskPlugins'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

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
      global: { plugins: [i18n], stubs: { RouterLink: true } },
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
})
