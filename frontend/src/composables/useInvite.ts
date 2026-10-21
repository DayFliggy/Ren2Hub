import { computed, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  affiliateApi,
  type AffiliateInfo,
  type AffiliateInvitee,
} from '@/api/affiliate'
import { ApiError } from '@/api/types'
import { useFeatureAccess } from '@/composables/useFeatureAccess'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { safeExternalUrl } from '@/utils/safeUrl'

export function useInvite() {
  const { t } = useI18n()
  const toast = useToast()
  const auth = useAuthStore()
  const { readOnly } = useFeatureAccess('invites', 'disabled')

  const info = ref<AffiliateInfo | null>(null)
  const loading = ref(true)
  const loadError = ref<string | null>(null)
  const inviteLink = ref('')

  const infoRequest = useLatestRequest()
  const inviteesRequest = useLatestRequest()
  const invitees = ref<AffiliateInvitee[]>([])
  const inviteesPage = ref(1)
  const inviteesTotal = ref(0)
  const inviteesPageSize = 20
  const inviteesOrder = ref<'asc' | 'desc'>('desc')
  const inviteesLoading = ref(false)
  const inviteesError = ref('')
  const inviteesPageCount = computed(() =>
    Math.max(1, Math.ceil(inviteesTotal.value / inviteesPageSize))
  )
  const transferOpen = ref(false)
  let transferOperation: string | null = null
  let disposed = false
  onScopeDispose(() => {
    disposed = true
  })
  const canTransfer = computed(
    () =>
      !readOnly.value &&
      info.value !== null &&
      BigInt(info.value.transferable) > 0n &&
      BigInt(auth.user?.quota ?? 0) + BigInt(info.value.transferable) <=
        BigInt(Number.MAX_SAFE_INTEGER)
  )
  const transferring = ref(false)

  async function load() {
    loading.value = true
    loadError.value = null
    const result = await infoRequest.run((signal) => affiliateApi.info(signal))
    if (result.stale) return
    loading.value = false
    if (!result.ok) {
      const message =
        result.error instanceof ApiError
          ? result.error.message
          : t('common.failed')
      loadError.value = message
      toast.error(message)
      return
    }
    info.value = result.value
    inviteLink.value = `${window.location.origin}/auth/sign-up?aff=${encodeURIComponent(result.value.code)}`
  }

  async function loadInvitees(nextPage = inviteesPage.value) {
    inviteesLoading.value = true
    inviteesError.value = ''
    const result = await inviteesRequest.run((signal) =>
      affiliateApi.invitees(
        nextPage,
        inviteesPageSize,
        inviteesOrder.value,
        signal
      )
    )
    if (result.stale) return
    inviteesLoading.value = false
    if (!result.ok) {
      inviteesError.value =
        result.error instanceof ApiError
          ? result.error.message
          : t('common.failed')
      return
    }
    invitees.value = result.value.items
    inviteesPage.value = result.value.page
    inviteesTotal.value = result.value.total
  }

  function sortInvitees(order: string) {
    if (order !== 'asc' && order !== 'desc') return
    inviteesOrder.value = order
    void loadInvitees(1)
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
      await affiliateApi.transfer(transferOperation)
      if (disposed) return
      transferOperation = null
      transferOpen.value = false
      toast.success(t('invite.transferSuccess'))
      await Promise.all([auth.fetchSelf(), load()])
    } catch (error) {
      if (!disposed)
        toast.error(error instanceof ApiError ? error.message : String(error))
    } finally {
      if (!disposed) transferring.value = false
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
    inviteesLoading,
    inviteesError,
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
