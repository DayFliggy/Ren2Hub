import { computed, ref } from 'vue'
import { api } from '@/api/console'
import { useAuthStore } from '@/stores/auth'
import { decimalToQuota } from '@/utils/quota'
export function useInvite() {
  const auth = useAuthStore(),
    code = ref(''),
    loading = ref(false),
    error = ref(''),
    amount = ref(''),
    transferring = ref(false)
  const inviteLink = computed(
    () =>
      `${window.location.origin}/sign-up?aff=${encodeURIComponent(code.value)}`
  )
  const quota = computed(() => {
    try {
      return Number(decimalToQuota(amount.value))
    } catch {
      return NaN
    }
  })
  const canTransfer = computed(
    () =>
      Number.isSafeInteger(quota.value) &&
      quota.value >= 500000 &&
      quota.value <= (auth.user?.aff_quota ?? 0)
  )
  async function load() {
    loading.value = true
    error.value = ''
    try {
      const [value] = await Promise.all([
        api.get<string>('/api/user/aff'),
        auth.fetchSelf(),
      ])
      code.value = value
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }
  async function transfer() {
    if (!canTransfer.value || transferring.value) return
    transferring.value = true
    error.value = ''
    try {
      await api.post('/api/user/aff_transfer', { quota: quota.value })
      amount.value = ''
      await load()
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      transferring.value = false
    }
  }
  return {
    code,
    inviteLink,
    loading,
    error,
    amount,
    transferring,
    canTransfer,
    load,
    transfer,
  }
}
