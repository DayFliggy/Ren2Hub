import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import InvoiceView from '../InvoiceView.vue'

afterEach(() => vi.unstubAllGlobals())

it.each([
  {
    locale: 'en',
    title: 'Invoicing',
    form: 'Invoice Request',
    records: 'My invoice requests',
    unavailable: 'Invoice records unavailable',
    note: 'Online submission is unavailable.',
  },
  {
    locale: 'zh-CN',
    title: '在线开票',
    form: '开票申请',
    records: '我的开票申请',
    unavailable: '开票记录暂不可用',
    note: '在线提交暂未开放。',
  },
])('preserves the invoice form and unavailable records in $locale', (copy) => {
  const wrapper = mount(InvoiceView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: copy.locale })],
    },
  })

  expect(wrapper.get('h1').text()).toBe(copy.title)
  expect(wrapper.text()).toContain(copy.form)
  expect(wrapper.text()).toContain(copy.records)
  expect(wrapper.get('[role="status"]').text()).toContain(copy.unavailable)
  expect(wrapper.get('#invoice-submit-note').text()).toContain(copy.note)
  expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBe('')
  expect(wrapper.findAll('input')).toHaveLength(4)
  expect(wrapper.findAll('textarea')).toHaveLength(1)
  expect(wrapper.find('a[download]').exists()).toBe(false)
  wrapper.unmount()
})

it('keeps an editable page-local draft without issuing invoice requests', async () => {
  const fetch = vi.fn(() => {
    throw new Error('Invoice requests must remain unavailable')
  })
  vi.stubGlobal('fetch', fetch)
  const i18n = createI18n({ legacy: false, locale: 'en' })
  const mountPage = () => mount(InvoiceView, { global: { plugins: [i18n] } })
  const wrapper = mountPage()
  const inputs = wrapper.findAll('input')
  await inputs[0]!.setValue('Draft company')
  await inputs[1]!.setValue('TAX-ID')
  await inputs[2]!.setValue('100.50')
  await inputs[3]!.setValue('billing@example.test')
  await wrapper.get('textarea').setValue('Draft only')
  await wrapper.get('form').trigger('submit')
  await flushPromises()

  expect((inputs[0]!.element as HTMLInputElement).value).toBe('Draft company')
  expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBe('')
  expect(fetch).not.toHaveBeenCalled()
  expect(wrapper.get('[role="status"]').text()).toContain(
    'Invoice records unavailable'
  )
  expect(wrapper.text()).not.toContain('No invoice requests yet')
  wrapper.unmount()

  const nextPage = mountPage()
  expect((nextPage.get('input').element as HTMLInputElement).value).toBe('')
  expect(fetch).not.toHaveBeenCalled()
  nextPage.unmount()
})
