import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import {
  parseSystemInstance,
  parseSystemTask,
  systemManagementApi,
} from '@/api/systemManagement'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import SystemManagementView from '@/views/admin/SystemManagementView.vue'

const node = parseSystemInstance({
  node_name: 'stale-node',
  status: 'stale',
  started_at: 1,
  last_seen_at: 2,
})
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.spyOn(systemManagementApi, 'instances').mockResolvedValue([node])
  vi.spyOn(systemManagementApi, 'tasks').mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})
function render() {
  wrapper = mount(SystemManagementView, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: {
            en: { common: { cancel: 'Cancel', confirm: 'Confirm' } },
          },
        }),
      ],
    },
  })
  return wrapper
}

describe('system instance destructive controls', () => {
  it('requires confirmation and retains the dialog when a stale node comes back online', async () => {
    const remove = vi
      .spyOn(systemManagementApi, 'deleteStale')
      .mockRejectedValue(new Error('instance is not stale or no longer exists'))
    const view = render()
    await flushPromises()
    await view
      .get('button[aria-label="Delete stale instance"]')
      .trigger('click')
    expect(remove).not.toHaveBeenCalled()
    const confirm = view.findAllComponents(ConfirmDialog)[0]
    expect(confirm.props('open')).toBe(true)
    expect(confirm.props('message')).toContain('stale-node')
    vi.mocked(systemManagementApi.instances).mockResolvedValue([
      { ...node, status: 'online' },
    ])
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(remove).toHaveBeenCalledWith('stale-node')
    expect(confirm.props('open')).toBe(true)
    expect(document.body.textContent).toContain('instance is not stale')
    expect(
      view.find('button[aria-label="Delete stale instance"]').exists()
    ).toBe(false)
    confirm.vm.$emit('cancel')
    await flushPromises()
    expect(confirm.props('open')).toBe(false)
  })

  it('shows the backend cleanup count after batch confirmation', async () => {
    const remove = vi
      .spyOn(systemManagementApi, 'deleteStale')
      .mockResolvedValue(3)
    const view = render()
    await flushPromises()
    const button = view
      .findAll('button')
      .find((item) => item.text().includes('Clean up stale instances'))!
    await button.trigger('click')
    expect(remove).not.toHaveBeenCalled()
    view.findAllComponents(ConfirmDialog)[0].vm.$emit('confirm')
    await flushPromises()
    expect(remove).toHaveBeenCalledWith(undefined)
    expect(view.text()).toContain('Deleted 3 stale instance records')
  })

  it('freezes the chosen log cutoff and does not write until explicitly confirmed', async () => {
    const cleanup = vi.spyOn(systemManagementApi, 'cleanup').mockResolvedValue(
      parseSystemTask({
        task_id: 'cleanup-1',
        type: 'log_cleanup',
        status: 'pending',
        created_at: 1,
        updated_at: 1,
      })
    )
    const view = render()
    await flushPromises()
    await view.get('.system-menu-content button').trigger('click')
    const input = document.querySelector<HTMLInputElement>(
      'input[type="datetime-local"]'
    )!
    input.value = '2025-01-02T10:30'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    document
      .getElementById('log-cleanup-form')!
      .dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await flushPromises()
    expect(cleanup).not.toHaveBeenCalled()
    const confirm = view.findAllComponents(ConfirmDialog)[1]
    expect(confirm.props('open')).toBe(true)
    expect(confirm.props('message')).toContain('permanently deleted')
    confirm.vm.$emit('confirm')
    await flushPromises()
    expect(cleanup).toHaveBeenCalledWith(
      Math.floor(Date.parse('2025-01-02T10:30') / 1000)
    )
    expect(view.text()).toContain('Log cleanup task created: cleanup-1')
  })
})
