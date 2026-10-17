import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import RequestDiagnosticsPanel from '../RequestDiagnosticsPanel.vue'
import RequestDiagnosticsBrowser from '../RequestDiagnosticsBrowser.vue'
import RequestDiagnosticsTimeline from '../RequestDiagnosticsTimeline.vue'
import type { RequestDiagnostic } from '@/api/requestDiagnostics'
import { ApiError } from '@/api/types'

const mocks = vi.hoisted(() => ({ get: vi.fn(), list: vi.fn() }))
vi.mock('@/api/requestDiagnostics', () => ({
  getRequestDiagnostic: mocks.get,
  listRequestDiagnostics: mocks.list,
}))
const diagnostic: RequestDiagnostic = {
  schema_version: 1,
  request_id: 'req-1',
  token_id: 7,
  trace_id: 'same-trace',
  thread_id: 'same-thread',
  model_name: 'model',
  created_at: 1700000000,
  wait_ms: 19,
  user_queue_ms: 7,
  attempts: [
    {
      sequence: 1,
      attempt_id: 'attempt-failed',
      success: false,
      error_class: 'rate_limit',
      error_http_status: 429,
      origin: 'upstream',
      latency_ms: 120,
      queue_ms: 12,
      send_started_at: 1700000000000,
      finished_at: 1700000000120,
      acceptance: 'rejected',
      retry_permission: 'allowed',
      response_committed: false,
      channel_id: 12,
      credential_id: 'credential-private',
    },
    {
      sequence: 2,
      attempt_id: 'attempt-succeeded',
      success: true,
      origin: 'upstream',
      latency_ms: 50,
      upstream_latency_ms: 35,
      queue_ms: 0,
      send_started_at: 1700000000130,
      first_output_at: 1700000000160,
      ttft_ms: 30,
      finished_at: 1700000000180,
      acceptance: 'accepted',
      retry_permission: 'denied',
      response_committed: true,
    },
  ],
}
const plugins = () => [
  createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
]
beforeEach(() => vi.resetAllMocks())

describe('request diagnostics', () => {
  it('does not correlate transient Playground requests across the zero Token namespace', () => {
    const wrapper = mount(RequestDiagnosticsTimeline, {
      props: { diagnostic: { ...diagnostic, token_id: 0 } },
      global: { plugins: plugins() },
    })
    expect(wrapper.text()).toContain('No persistent Token')
    expect(wrapper.text()).toContain('View diagnostics by request ID')
    expect(wrapper.text()).toContain('Wait budget used')
    expect(wrapper.text()).not.toContain('Total wait')
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.text()).toContain('req-1')
    wrapper.unmount()
  })
  it('shows unavailable for absent identity or 404 and does not fabricate attempts', async () => {
    const wrapper = mount(RequestDiagnosticsPanel, {
      global: { plugins: plugins() },
    })
    expect(mocks.get).not.toHaveBeenCalled()
    expect(wrapper.find('[data-diagnostics-unavailable]').exists()).toBe(true)
    mocks.get.mockRejectedValue(new ApiError('not found', { status: 404 }))
    await wrapper.setProps({ requestId: 'expired' })
    await flushPromises()
    expect(wrapper.find('[data-diagnostics-unavailable]').exists()).toBe(true)
    expect(wrapper.find('[data-diagnostic-attempt]').exists()).toBe(false)
    expect(wrapper.text()).toContain('7 days')
    wrapper.unmount()
  })
  it('aborts stale detail requests and preserves failure-to-success order', async () => {
    let finishOld!: (value: RequestDiagnostic) => void
    mocks.get
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishOld = resolve
          })
      )
      .mockResolvedValueOnce(diagnostic)
    const wrapper = mount(RequestDiagnosticsPanel, {
      props: { requestId: 'old' },
      global: { plugins: plugins() },
    })
    const signal = mocks.get.mock.calls[0]![2] as AbortSignal
    await wrapper.setProps({ requestId: 'req-1' })
    await flushPromises()
    expect(signal.aborted).toBe(true)
    finishOld({ ...diagnostic, request_id: 'stale-response' })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale-response')
    const attempts = wrapper.findAll('[data-diagnostic-attempt]')
    expect(attempts).toHaveLength(2)
    expect(attempts[0]!.text()).toContain('Failed')
    expect(attempts[0]!.get('[data-upstream-duration]').text()).toBe('Unknown')
    expect(attempts[1]!.text()).toContain('Succeeded')
    expect(attempts[1]!.get('[data-upstream-duration]').text()).toBe('35 ms')
    expect(attempts[1]!.text()).toContain('Total attempt duration')
    expect(wrapper.text()).not.toContain('credential-private')
    wrapper.unmount()
  })
  it('carries Token identity when following correlation links', async () => {
    const wrapper = mount(RequestDiagnosticsTimeline, {
      props: { diagnostic, admin: true },
      global: { plugins: plugins() },
    })
    expect(wrapper.text()).toContain('credential-private')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Requests in this Trace')!
      .trigger('click')
    expect(wrapper.emitted('related')?.[0]).toEqual([
      { token_id: 7, trace_id: 'same-trace' },
    ])
    await wrapper.setProps({ admin: false })
    expect(wrapper.text()).not.toContain('credential-private')
    wrapper.unmount()
  })
  it('requires a Token filter and defaults to self even for administrators', async () => {
    mocks.list.mockResolvedValue({
      items: [diagnostic],
      total: 1,
      page: 1,
      pageSize: 10,
    })
    const wrapper = mount(RequestDiagnosticsBrowser, {
      props: { canReadAll: true },
      global: {
        plugins: plugins(),
        stubs: {
          ConsoleModal: { template: '<div><slot/><slot name="footer"/></div>' },
        },
      },
    })
    expect(mocks.list).not.toHaveBeenCalled()
    await wrapper.get('form').trigger('submit')
    expect(wrapper.get('[role="alert"]').text()).toContain('valid Token ID')
    expect(mocks.list).not.toHaveBeenCalled()
    await wrapper.get('input[aria-label="Token ID"]').setValue('7')
    await wrapper.get('input[aria-label="Trace ID"]').setValue('same-trace')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith(
      { token_id: 7, trace_id: 'same-trace' },
      1,
      10,
      false,
      expect.any(AbortSignal)
    )
    await wrapper.get('select[aria-label="Query scope"]').setValue('all')
    await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith(
      { token_id: 7, trace_id: 'same-trace' },
      1,
      10,
      true,
      expect.any(AbortSignal)
    )
    await wrapper.setProps({ canReadAll: false })
    await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith(
      { token_id: 7, trace_id: 'same-trace' },
      1,
      10,
      false,
      expect.any(AbortSignal)
    )
    expect(wrapper.find('select[aria-label="Query scope"]').exists()).toBe(
      false
    )
    wrapper.unmount()
  })
})
