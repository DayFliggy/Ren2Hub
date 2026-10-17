import { ref, watch } from 'vue'
import { api } from '@/api/console'
import { useLatestRequest } from './useLatestRequest'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
export interface CheckinStatus {
  enabled: boolean
  min_quota: number
  max_quota: number
  stats: {
    total_quota: number
    total_checkins: number
    checkin_count: number
    checked_in_today: boolean
    records: Array<{ checkin_date: string; quota_awarded: number }>
  }
}
export function useActivity() {
  const now = new Date(),
    month = ref(
      `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
    )
  const status = ref<CheckinStatus | null>(null),
    loading = ref(false),
    claiming = ref(false),
    loadError = ref<string | null>(null)
  const request = useLatestRequest(),
    auth = useAuthStore(),
    app = useAppStore()
  async function load() {
    if (!app.checkinEnabled) {
      request.cancel()
      status.value = null
      loading.value = false
      loadError.value = null
      return
    }
    loading.value = true
    loadError.value = null
    const result = await request.run((signal) =>
      api.get<CheckinStatus>(
        '/api/user/checkin',
        { month: month.value },
        { signal }
      )
    )
    if (result.stale) return
    loading.value = false
    if (result.ok) status.value = result.value
    else
      loadError.value =
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
  }
  async function checkin(turnstileToken?: string) {
    if (!app.checkinEnabled || !status.value?.enabled || claiming.value) return
    claiming.value = true
    loadError.value = null
    try {
      await api.post(
        '/api/user/checkin',
        undefined,
        turnstileToken
          ? { headers: { 'X-Turnstile-Token': turnstileToken } }
          : undefined
      )
      await Promise.all([load(), auth.fetchSelf()])
    } catch (error) {
      loadError.value = error instanceof Error ? error.message : String(error)
    } finally {
      claiming.value = false
    }
  }
  watch([() => app.checkinEnabled, month], load, { immediate: true })
  return { month, status, loading, claiming, loadError, load, checkin }
}
