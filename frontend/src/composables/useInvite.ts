import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import {
  invalidResponse,
  isRecord,
  parsePage,
  requiredStrictInteger,
  requiredString,
} from '@/api/contracts'
import { ApiError } from '@/api/types'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { useFeatureAccess } from '@/composables/useFeatureAccess'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import type { Invitee, InviteInfo } from '@/types/console'
import { MAX_QUOTA } from '@/utils/quota'
import { safeExternalUrl } from '@/utils/safeUrl'

export function useInvite() {
  const { t } = useI18n()
  const toast = useToast()
  const auth = useAuthStore()
  const { readOnly } = useFeatureAccess('invites', 'disabled')

  const info = ref<InviteInfo | null>(null)
  const loading = ref(true)
  const loadError = ref<string | null>(null)
  const inviteLink = ref('')
  const invitees = ref<Invitee[]>([])
  const inviteesPage = ref(1)
  const inviteesTotal = ref(0)
  const inviteesPageSize = 20
  const inviteesOrder = ref<'desc' | 'asc'>('desc')
  const inviteesRequest = useAdminRequest()
  const inviteesPageCount = computed(() =>
    Math.max(1, Math.ceil(inviteesTotal.value / inviteesPageSize))
  )

  async function loadInvitees(page = inviteesPage.value) {
    const endpoint = '/api/user/affiliate/invitees'
    await inviteesRequest.run(async (signal) => {
      const query = new URLSearchParams({
        p: String(page),
        page_size: String(inviteesPageSize),
        order: inviteesOrder.value,
      })
      const data = await api.get<unknown>(`${endpoint}?${query}`, undefined, {
        signal,
      })
      const result = parsePage(data, endpoint, (item) => {
        if (!isRecord(item)) invalidResponse(endpoint)
        return {
          masked_name: requiredString(item.masked_name, endpoint),
          invited_at: requiredStrictInteger(item.invited_at, endpoint),
          reward_quota: requiredStrictInteger(item.reward_quota, endpoint),
        }
      })
      if (signal.aborted) return
      invitees.value = result.items
      inviteesPage.value = result.page
      inviteesTotal.value = result.total
    })
  }

  async function sortInvitees(order: 'desc' | 'asc') {
    inviteesOrder.value = order
    await loadInvitees(1)
  }

  const transferOpen = ref(false)
  let transferOperation: string | null = null
  const canTransfer = computed(
    () =>
      !readOnly.value &&
      (info.value?.transferable ?? 0) > 0 &&
      BigInt(auth.user?.quota ?? 0) + BigInt(info.value?.transferable ?? 0) <=
        MAX_QUOTA
  )
  const transferring = ref(false)

  async function load() {
    loading.value = true
    loadError.value = null
    try {
      const data = await api.get<InviteInfo>('/api/user/affiliate')
      info.value = data
      inviteLink.value = `${window.location.origin}/auth/sign-up?aff=${data.code}`
    } catch (error) {
      const message =
        error instanceof ApiError ? error.message : t('common.failed')
      loadError.value = message
      toast.error(message)
    } finally {
      loading.value = false
    }
  }

  async function writeClipboard(text: string) {
    try {
      await navigator.clipboard.writeText(text)
      toast.success(t('common.copied'))
    } catch {
      toast.error(t('common.copyFailed'))
    }
  }

  async function copyCode() {
    if (info.value) await writeClipboard(info.value.code)
  }

  async function copyLink() {
    if (inviteLink.value) await writeClipboard(inviteLink.value)
  }

  function shareText() {
    return t('invite.shareText', {
      reward: `${info.value?.effective_rate_percent ?? 0}%`,
    })
  }

  function openShare(url: string, origin: string) {
    const safeUrl = safeExternalUrl(url, [origin])
    if (!safeUrl) {
      toast.error(t('common.failed'))
      return
    }
    window.open(safeUrl, '_blank', 'noopener,noreferrer')
  }

  function shareX() {
    openShare(
      `https://twitter.com/intent/tweet?text=${encodeURIComponent(shareText())}&url=${encodeURIComponent(inviteLink.value)}`,
      'https://twitter.com'
    )
  }

  function shareTelegram() {
    openShare(
      `https://t.me/share/url?url=${encodeURIComponent(inviteLink.value)}&text=${encodeURIComponent(shareText())}`,
      'https://t.me'
    )
  }

  function shareEmail() {
    window.location.href = `mailto:?subject=${encodeURIComponent(shareText())}&body=${encodeURIComponent(inviteLink.value)}`
  }

  async function transfer() {
    if (!canTransfer.value || transferring.value) return
    transferOperation ??= crypto.randomUUID()
    transferring.value = true
    try {
      await api.post('/api/user/affiliate/transfer', {
        operation_id: transferOperation,
      })
      toast.success(t('invite.transferSuccess'))
      transferOpen.value = false
      transferOperation = null
      await Promise.all([auth.fetchSelf(), load()])
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : String(error))
    } finally {
      transferring.value = false
    }
  }

  return {
    info,
    loading,
    loadError,
    inviteLink,
    invitees,
    inviteesPage,
    inviteesTotal,
    inviteesPageCount,
    inviteesOrder,
    inviteesLoading: inviteesRequest.loading,
    inviteesError: inviteesRequest.error,
    loadInvitees,
    sortInvitees,
    transferOpen,
    canTransfer,
    transferring,
    readOnly,
    load,
    copyCode,
    copyLink,
    shareX,
    shareTelegram,
    shareEmail,
    transfer,
  }
}
