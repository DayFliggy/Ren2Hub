import { mount } from '@vue/test-utils'
import { beforeAll, expect, it } from 'vitest'
import FetchedModelsDialog from '../FetchedModelsDialog.vue'
import { resolveModelProvider } from '@/utils/modelProvider'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
it('classifies overlapping aliases before broad provider fallbacks', () => {
  for (const [name, expected] of [
    ['text-embedding-v3', 'Qwen'],
    ['step-tts-mini', 'StepFun'],
    ['360gpt-pro', '360 AI'],
    ['sonar-llama-3', 'Perplexity'],
    ['nvidia/llama-3-nemotron', 'NVIDIA'],
    ['wan2.6-t2v', 'Wan'],
    ['gpt-4.1', 'OpenAI'],
  ])
    expect(resolveModelProvider(name!)?.name).toBe(expected)
  expect(resolveModelProvider('custom-model')).toBeNull()
})
it('keeps selection local until applied and groups filtered selections', async () => {
  const wrapper = mount(FetchedModelsDialog, {
    props: { open: true, models: ['gpt-4.1', 'gpt-4o', 'wan2.6-t2v'] },
    global: {
      plugins: [i18n],
      stubs: {
        ConsoleModal: { template: '<div><slot/><slot name="footer"/></div>' },
      },
    },
  })
  expect(wrapper.emitted('apply')).toBeUndefined()
  await wrapper.get('input[aria-label="Select OpenAI models"]').setValue(false)
  await wrapper.get('[data-apply-fetched-models]').trigger('click')
  expect(wrapper.emitted('apply')?.[0]?.[0]).toEqual(['wan2.6-t2v'])
  wrapper.unmount()
})
