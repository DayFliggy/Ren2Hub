<script setup lang="ts">
import { ChevronRight } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminChannel } from '@/types/console'
import { localizedText } from '@/utils/localizedText'
import { formatMoney, formatTime } from '@/utils/format'
const props = defineProps<{ channel: AdminChannel }>()
const { t, locale } = useI18n()
type Balance = {
  amount?: number
  currency?: string
  unlimited?: boolean
  windows?: Array<{
    name?: string | Record<string, string>
    used: number
    limit: number
    unit?: string
    resetAt?: number
  }>
  error?: string
}
const parsed = computed(() => {
  try {
    const value = JSON.parse(props.channel.other_info || '{}')
    return { detail: value.balance_detail as Balance | undefined, error: '' }
  } catch {
    return { detail: undefined, error: t('pluginExtensions.invalidBalance') }
  }
})
function summary(value?: Balance): string {
  if (value?.unlimited) return t('pluginExtensions.unlimited')
  if (typeof value?.amount === 'number' && Number.isFinite(value.amount)) {
    const currency = value.currency?.trim() || 'USD'
    if (currency === 'USD') return formatMoney(value.amount)
    try {
      return new Intl.NumberFormat(locale.value, {
        style: 'currency',
        currency,
      }).format(value.amount)
    } catch {
      return `${value.amount.toLocaleString(locale.value)} ${currency}`
    }
  }
  const percents = (value?.windows ?? [])
    .filter((item) => item.limit > 0)
    .map((item) => Math.round((item.used / item.limit) * 100))
  return percents.length
    ? t('pluginExtensions.percentUsed', { percent: Math.max(...percents) })
    : formatMoney(props.channel.balance)
}
const detail = computed(() => parsed.value.detail)
</script>
<template>
  <div class="min-w-0 text-xs">
    <span
      v-if="parsed.error"
      role="alert"
      class="text-[var(--status-danger-text)]"
      >{{ parsed.error }}</span
    >
    <details v-else-if="detail" class="balance-details break-words">
      <summary
        class="balance-details-summary inline-flex cursor-pointer items-center gap-1 font-semibold tabular-nums focus-ring"
        :title="t('channels.balanceDetailsToggle')"
      >
        <span>{{ summary(detail) }}</span>
        <ChevronRight
          :size="12"
          class="balance-details-chevron shrink-0 text-[var(--text-tertiary)] transition-transform"
          aria-hidden="true"
        />
      </summary>
      <p v-if="detail.error" class="mt-1 text-[var(--status-danger-text)]">
        {{ detail.error }}
      </p>
      <p
        v-for="(window, index) in detail.windows"
        :key="index"
        class="mt-1 text-[0.6875rem] text-[var(--text-secondary)]"
      >
        {{ localizedText(window.name, locale) }} {{ window.used }} /
        {{ window.limit }} {{ window.unit
        }}<span v-if="window.resetAt"> · {{ formatTime(window.resetAt) }}</span>
      </p>
    </details>
    <span v-else class="font-semibold tabular-nums">{{
      formatMoney(channel.balance)
    }}</span>
  </div>
</template>

<style scoped>
.balance-details-summary {
  list-style: none;
}
.balance-details-summary::-webkit-details-marker {
  display: none;
}
.balance-details[open] .balance-details-chevron {
  transform: rotate(90deg);
}
</style>
