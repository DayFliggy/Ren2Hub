import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import AuthEditorialPanel from '@/components/auth/AuthEditorialPanel.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'

const COMMIT = 'c7b249bb216d82d13e7cb33946172c9644896d4a'

function mountPanel(state: {
  registerEnabled: boolean
  uptimePercent: number | null
  version: string
}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore()
  app.status = {
    register_enabled: state.registerEnabled,
    version: state.version,
  }
  app.statusReachable = true
  app.uptimePercent = state.uptimePercent
  return mount(AuthEditorialPanel, {
    global: { plugins: [pinia, i18n], stubs: { AuthRelayFlow: true } },
  })
}

describe('AuthEditorialPanel', () => {
  beforeEach(async () => {
    await loadMessageDomain('auth')
    await setLocale('zh-CN')
  })

  it('makes no open-registration claim while registration is closed', () => {
    const closed = mountPanel({
      registerEnabled: false,
      uptimePercent: null,
      version: 'v1.0.0',
    })
    const open = mountPanel({
      registerEnabled: true,
      uptimePercent: null,
      version: 'v1.0.0',
    })

    expect(closed.find('[data-registration-claim]').exists()).toBe(false)
    expect(open.get('[data-registration-claim]').text()).toBe(
      i18n.global.t('auth.editorial.point3')
    )
  })

  it('hides availability when no uptime data is available', () => {
    const uptimeLabel = i18n.global.t('auth.tickerUptime')
    const hidden = mountPanel({
      registerEnabled: false,
      uptimePercent: null,
      version: 'v1.0.0',
    })
    const shown = mountPanel({
      registerEnabled: false,
      uptimePercent: 99.9,
      version: 'v1.0.0',
    })

    expect(hidden.text()).not.toContain(uptimeLabel)
    expect(shown.text()).toContain(uptimeLabel)
    expect(shown.text()).toContain('99.90%')
  })

  it('shortens a commit-hash version and keeps the full value as a tooltip', () => {
    const wrapper = mountPanel({
      registerEnabled: false,
      uptimePercent: null,
      version: COMMIT,
    })

    const version = wrapper.get(`[title="${COMMIT}"]`)
    expect(version.text()).toBe(COMMIT.slice(0, 9))
    expect(wrapper.text()).not.toContain(COMMIT)
  })
})
