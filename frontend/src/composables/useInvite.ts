import { computed, onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  affiliateApi,
  type AffiliateInfo,
  type AffiliateInvitee,
  type AffiliateRecord,
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
  // Registration rejects the code when either switch is off, so every copy and
  // share action is gated here. Transferring existing rewards stays allowed.
  const shareable = computed(
    () =>
      info.value !== null &&
      info.value.policy.enabled &&
      info.value.code_enabled
  )
  const shareNotice = computed(() => {
    if (!info.value || shareable.value) return ''
    return info.value.policy.enabled
      ? t('invite.codeDisabled')
      : t('invite.programDisabled')
  })
  const inviteLink = computed(() =>
    shareable.value && info.value
      ? `${window.location.origin}/auth/sign-up?aff=${encodeURIComponent(info.value.code)}`
      : ''
  )

  const infoRequest = useLatestRequest()
  const inviteesRequest = useLatestRequest()
  const recordsRequest = useLatestRequest()
  const records = ref<AffiliateRecord[]>([])
  const recordsPage = ref(1)
  const recordsTotal = ref(0)
  const recordsLoading = ref(false)
  const recordsError = ref('')
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
      info.value.transfer_enabled &&
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
  }

  let requestedInviteesPage = 1
  async function loadInvitees(nextPage = requestedInviteesPage) {
    requestedInviteesPage = nextPage
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
      invitees.value = []
      inviteesPage.value = 1
      inviteesTotal.value = 0
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

  async function loadRecords(nextPage = recordsPage.value) {
    recordsLoading.value = true
    recordsError.value = ''
    const result = await recordsRequest.run((signal) =>
      affiliateApi.records(nextPage, 20, signal)
    )
    if (result.stale) return
    recordsLoading.value = false
    if (!result.ok) {
      records.value = []
      recordsTotal.value = 0
      recordsError.value =
        result.error instanceof ApiError
          ? result.error.message
          : t('common.failed')
      return
    }
    records.value = result.value.items
    recordsPage.value = result.value.page
    recordsTotal.value = result.value.total
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
    if (shareable.value && info.value) await writeClipboard(info.value.code)
  }

  async function copyLink() {
    if (shareable.value) await writeClipboard(inviteLink.value)
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
    if (!shareable.value) return
    openShare(
      `https://twitter.com/intent/tweet?text=${encodeURIComponent(shareText())}&url=${encodeURIComponent(inviteLink.value)}`,
      'https://twitter.com'
    )
  }

  function shareTelegram() {
    if (!shareable.value) return
    openShare(
      `https://t.me/share/url?url=${encodeURIComponent(inviteLink.value)}&text=${encodeURIComponent(shareText())}`,
      'https://t.me'
    )
  }

  function shareEmail() {
    if (!shareable.value) return
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
      await Promise.all([auth.fetchSelf(), load(), loadRecords(1)])
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
    shareable,
    shareNotice,
    inviteLink,
    invitees,
    inviteesPage,
    inviteesTotal,
    inviteesPageCount,
    inviteesOrder,
    inviteesLoading,
    inviteesError,
    loadInvitees,
    records,
    recordsPage,
    recordsTotal,
    recordsLoading,
    recordsError,
    loadRecords,
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
