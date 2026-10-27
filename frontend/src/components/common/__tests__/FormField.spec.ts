import { defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FormField from '@/components/common/FormField.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'

function mountSelect(group: boolean) {
  const value = ref('csv')
  const wrapper = mount(
    defineComponent({
      components: { FormField, FilterSelect },
      setup: () => ({ value, group }),
      template: `
        <FormField label="Format" :group="group">
          <FilterSelect
            v-model="value"
            label="Format"
            :options="[{ value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }]"
          />
        </FormField>
      `,
    }),
    { attachTo: document.body }
  )
  return { wrapper, value }
}

describe('FormField', () => {
  it('lets a grouped combobox option close the list instead of reopening it', async () => {
    const { wrapper, value } = mountSelect(true)
    try {
      await wrapper.get('[role="combobox"]').trigger('click')
      ;(wrapper.findAll('[role="option"]')[1]!.element as HTMLElement).click()
      await wrapper.vm.$nextTick()

      expect(value.value).toBe('json')
      expect(wrapper.get('[role="combobox"]').attributes('aria-expanded')).toBe(
        'false'
      )
      const group = wrapper.get('[role="group"]')
      expect(group.element.tagName).toBe('DIV')
      expect(
        document.getElementById(group.attributes('aria-labelledby')!)
          ?.textContent
      ).toContain('Format')
    } finally {
      wrapper.unmount()
    }
  })

  it('keeps the wrapping label for a single native control', () => {
    const wrapper = mount(FormField, {
      props: { label: 'Name' },
      slots: { default: '<input />' },
    })
    expect(wrapper.element.tagName).toBe('LABEL')
    expect(wrapper.attributes('role')).toBeUndefined()
  })
})
