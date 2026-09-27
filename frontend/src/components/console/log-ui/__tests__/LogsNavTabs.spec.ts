import { defineComponent } from 'vue'
import { beforeAll, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

import LogsNavTabs from '@/components/console/log-ui/LogsNavTabs.vue'
import i18n, { loadMessageDomain } from '@/i18n'
import { parsePublicStatus } from '@/api/public'
import { useAppStore } from '@/stores/app'

const EmptyRoute = defineComponent({ template: '<div />' })

async function mountTabs(
  capabilities: Record<string, string>,
  active: 'consume' | 'audit' = 'consume'
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAppStore().status = parsePublicStatus({
    frontend_capabilities: capabilities,
  })

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/logs', name: 'logs', component: EmptyRoute },
      { path: '/logs/drawing', name: 'logs-drawing', component: EmptyRoute },
      { path: '/logs/tasks', name: 'logs-tasks', component: EmptyRoute },
      { path: '/logs/audit', name: 'logs-audit', component: EmptyRoute },
    ],
  })
  await router.push('/logs')
  await router.isReady()

  return mount(LogsNavTabs, {
    props: { active },
    global: { plugins: [pinia, router, i18n] },
  })
}

beforeAll(async () => {
  i18n.global.locale.value = 'zh-CN'
  await loadMessageDomain('console')
})

describe('audit log navigation tab', () => {
  it('shows the audit tab whenever the audit capability is live', async () => {
    const wrapper = await mountTabs({ logs: 'live', audit: 'live' })
    const audit = wrapper.find('a[href="/logs/audit"]')
    expect(wrapper.findAll('a')).toHaveLength(4)
    expect(audit.exists()).toBe(true)
    expect(audit.text()).toBe('审计日志')
  })

  it.each<Record<string, string>>([{ audit: 'disabled' }, {}])(
    'hides the audit tab when the capability is %j',
    async (extra) => {
      const wrapper = await mountTabs({ logs: 'live', ...extra })
      expect(wrapper.findAll('a')).toHaveLength(3)
      expect(wrapper.find('a[href="/logs/audit"]').exists()).toBe(false)
    }
  )

  it('marks the audit tab as the current page', async () => {
    const wrapper = await mountTabs({ audit: 'live' }, 'audit')
    expect(
      wrapper.find('a[href="/logs/audit"]').attributes('aria-current')
    ).toBe('page')
  })
})
