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
  active: 'calls' | 'tasks' | 'quota' | 'audit' = 'calls'
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAppStore().status = parsePublicStatus({
    frontend_capabilities: capabilities,
  })

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/usage-logs', name: 'logs', component: EmptyRoute },
      { path: '/usage-logs/task', name: 'logs-tasks', component: EmptyRoute },
      { path: '/usage-logs/quota', name: 'logs-quota', component: EmptyRoute },
      { path: '/usage-logs/audit', name: 'logs-audit', component: EmptyRoute },
    ],
  })
  await router.push('/usage-logs')
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

describe('log center navigation tabs', () => {
  it('renders the four primary categories when audit capability is live', async () => {
    const wrapper = await mountTabs({ logs: 'live', audit: 'live' })
    const links = wrapper.findAll('a')
    expect(links.map((item) => item.text())).toEqual([
      '调用日志',
      '异步任务',
      '额度变动',
      '审计日志',
    ])
    expect(wrapper.find('a[href="/usage-logs/quota"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/usage-logs/audit"]').exists()).toBe(true)
  })

  it.each<Record<string, string>>([{ audit: 'disabled' }, {}])(
    'hides the audit tab when the capability is %j',
    async (extra) => {
      const wrapper = await mountTabs({ logs: 'live', ...extra })
      expect(wrapper.findAll('a')).toHaveLength(3)
      expect(wrapper.find('a[href="/usage-logs/audit"]').exists()).toBe(false)
    }
  )

  it('marks the active tab as the current page', async () => {
    const auditWrapper = await mountTabs({ audit: 'live' }, 'audit')
    expect(
      auditWrapper
        .find('a[href="/usage-logs/audit"]')
        .attributes('aria-current')
    ).toBe('page')

    const quotaWrapper = await mountTabs({ audit: 'live' }, 'quota')
    expect(
      quotaWrapper
        .find('a[href="/usage-logs/quota"]')
        .attributes('aria-current')
    ).toBe('page')
  })
})
