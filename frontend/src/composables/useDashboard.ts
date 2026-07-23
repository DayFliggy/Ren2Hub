import { ref } from 'vue'
import { useLocalStorage } from '@vueuse/core'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import type { LogItem } from '@/types/console'

export interface DashboardStats {
  quota: number
  used_quota: number
  today_quota: number
  today_requests: number
  total_requests: number
  month_quota_delta: number
  month_requests_delta: number
}

export interface ModelShare {
  model: string
  ratio: number
  quota: number
}

export interface FlowPoint {
  date: string
  consume: number
  requests: number
  topup: number
}

/** Balance visibility preference, shared across dashboard & wallet. */
export function useBalanceVisibility() {
  const hidden = useLocalStorage('renren_hide_balance', false)
  return { hidden, toggle: () => (hidden.value = !hidden.value) }
}

export function useDashboard() {
  const { t } = useI18n()
  const toast = useToast()
  const loading = ref(true)
  const stats = ref<DashboardStats | null>(null)
  const share = ref<ModelShare[]>([])
  const flow = ref<FlowPoint[]>([])
  const recent = ref<LogItem[]>([])

  async function load() {
    loading.value = true
    try {
      const [dataSelf, flowSelf, logSelf] = await Promise.all([
        api.get<DashboardStats & { model_share: ModelShare[] }>(
          '/api/data/self'
        ),
        api.get<FlowPoint[]>('/api/data/flow/self'),
        api.get<{ items: LogItem[] }>('/api/log/self', {
          page: 1,
          page_size: 6,
        }),
      ])
      const { model_share, ...rest } = dataSelf
      stats.value = rest
      share.value = model_share
      flow.value = flowSelf
      recent.value = logSelf.items
    } catch (error) {
      toast.error(
        error instanceof ApiError ? error.message : t('common.failed')
      )
    } finally {
      loading.value = false
    }
  }

  return { loading, stats, share, flow, recent, load }
}
