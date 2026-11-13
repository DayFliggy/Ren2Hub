<script setup lang="ts">
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
  keys?: Record<string, Balance>
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
      class="text-[var(--status-danger)]"
      >{{ parsed.error }}</span
    >
    <details v-else-if="detail" class="break-words">
      <summary class="cursor-pointer font-semibold tabular-nums">
        {{ summary(detail) }}
      </summary>
      <p v-if="detail.error" class="text-[var(--status-danger)]">
        {{ detail.error }}
      </p>
      <p v-for="(window, index) in detail.windows" :key="index" class="mt-1">
        {{ localizedText(window.name, locale) }} {{ window.used }} /
        {{ window.limit }} {{ window.unit
        }}<span v-if="window.resetAt"> · {{ formatTime(window.resetAt) }}</span>
      </p>
      <div
        v-for="(key, index) in detail.keys"
        :key="index"
        class="mt-1 border-t border-[var(--border-subtle)] pt-1"
      >
        <span
          >{{ t('pluginExtensions.credential') }} {{ Number(index) + 1 }} ·
          {{ summary(key) }}</span
        >
        <p v-if="key.error" class="text-[var(--status-danger)]">
          {{ key.error }}
        </p>
        <p v-for="(window, i) in key.windows" :key="i">
          {{ localizedText(window.name, locale) }} {{ window.used }} /
          {{ window.limit }} {{ window.unit
          }}<span v-if="window.resetAt">
            · {{ formatTime(window.resetAt) }}</span
          >
        </p>
      </div>
    </details>
    <span v-else class="font-semibold tabular-nums">{{
      formatMoney(channel.balance)
    }}</span>
  </div>
</template>
