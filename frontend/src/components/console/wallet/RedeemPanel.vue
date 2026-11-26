<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight } from 'lucide-vue-next'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import { parseRedemption } from '@/api/liveContracts'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { formatQuota } from '@/utils/format'

const props = withDefaults(
  defineProps<{
    autoFocus?: boolean
    refreshKey?: number
  }>(),
  { autoFocus: false, refreshKey: 0 }
)

const emit = defineEmits<{ done: [] }>()
const { t } = useI18n()
const toast = useToast()
const auth = useAuthStore()

const code = ref('')
const submittingRedeem = ref(false)
const balanceRefreshFailed = ref(false)
const refreshingBalance = ref(false)
let focusTimer: number | null = null

async function redeem(): Promise<void> {
  const trimmed = code.value.trim()
  if (!trimmed) return
  submittingRedeem.value = true
  try {
    // The bonus sits next to data, so the whole envelope is read.
    const response = await api.postEnvelope<unknown>('/api/user/topup', {
      key: trimmed,
    })
    const redeemed = parseRedemption(response)
    toast.success(
      BigInt(redeemed.bonus_quota) > 0n
        ? t('wallet.redeemedWithBonus', {
            quota: formatQuota(redeemed.quota),
            bonus: formatQuota(redeemed.bonus_quota),
          })
        : t('wallet.redeemed', { quota: formatQuota(redeemed.quota) })
    )
    code.value = ''
    emit('done')
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
    return
  } finally {
    submittingRedeem.value = false
  }
  await refreshBalance()
}

/** The code is already spent, so a failure here only retries the balance read. */
async function refreshBalance(): Promise<void> {
  refreshingBalance.value = true
  try {
    await auth.fetchSelf()
    balanceRefreshFailed.value = false
  } catch {
    balanceRefreshFailed.value = true
  } finally {
    refreshingBalance.value = false
  }
}

function focusInput(): void {
  if (focusTimer !== null) window.clearTimeout(focusTimer)
  focusTimer = null
  if (!props.autoFocus) return
  focusTimer = window.setTimeout(() => {
    focusTimer = null
    document.getElementById('redeem-input')?.focus()
  }, 300)
}

watch(() => props.autoFocus, focusInput, { immediate: true })
onBeforeUnmount(() => {
  if (focusTimer !== null) window.clearTimeout(focusTimer)
})
</script>

<template>
  <ConsoleCard>
    <form
      class="flex flex-col gap-4 md:flex-row md:items-center"
      @submit.prevent="redeem"
    >
      <div class="flex shrink-0 items-center gap-2">
        <h2 class="text-sm font-semibold text-[var(--text-primary)]">
          {{ t('wallet.redeemTitle') }}
        </h2>
        <StatusChip tone="accent">
          {{ t('wallet.redeemLimitOnce') }}
        </StatusChip>
      </div>
      <TextInput
        id="redeem-input"
        v-model="code"
        class="min-w-0 md:flex-1"
        :aria-label="t('wallet.redeemTitle')"
        :placeholder="t('wallet.redeemPlaceholder')"
      />
      <ConsoleButton
        type="submit"
        class="w-full md:w-auto md:shrink-0"
        variant="secondary"
        :loading="submittingRedeem"
        :disabled="!code.trim()"
      >
        <ArrowRight :size="15" aria-hidden="true" />
        {{ t('wallet.redeemNow') }}
      </ConsoleButton>
    </form>
    <div
      v-if="balanceRefreshFailed"
      role="alert"
      class="mt-3 flex flex-wrap items-center gap-3 text-sm text-[var(--status-danger-text)]"
    >
      <span>{{ t('wallet.redeemedBalanceRefreshFailed') }}</span>
      <ConsoleButton
        size="sm"
        variant="secondary"
        :loading="refreshingBalance"
        @click="refreshBalance"
      >
        {{ t('wallet.refreshBalance') }}
      </ConsoleButton>
    </div>
  </ConsoleCard>
</template>
