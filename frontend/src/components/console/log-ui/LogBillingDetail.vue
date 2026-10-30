<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { logEvidence } from '@/utils/logModel'
import type { LogItem } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { rateToPercent } from '@/features/usage-discount/amount'
import {
  LOG_COMPONENTS,
  logUsageDiscount,
  logUsageDiscountAdmin,
  type LogComponent,
} from '@/features/usage-discount/log'
const props = defineProps<{ log: LogItem }>(),
  { t } = useI18n()
const details = computed(() => logEvidence(props.log))
// channel_ratio in the evidence is already base × factor; the summary
// explains how it was derived.
const discount = computed(() => logUsageDiscount(details.value))
const discountAdmin = computed(() => logUsageDiscountAdmin(details.value))
const discountStatus = computed(
  () => discount.value?.status ?? discountAdmin.value?.status
)
function percent(rate: string) {
  return `${rateToPercent(rate)}%`
}
function part(name: LogComponent) {
  return t(`billing.usageDiscount.${name}`)
}
// Components always read in LOG_COMPONENTS order, whatever the JSON order.
function parts(rates: Partial<Record<LogComponent, string>>) {
  return LOG_COMPONENTS.filter((name) => rates[name] !== undefined)
    .map((name) => `${part(name)} ${percent(rates[name]!)}`)
    .join(' · ')
}
</script>
<template>
  <div class="space-y-4 text-left">
    <dl class="grid grid-cols-[auto_1fr] gap-3 text-sm">
      <dt>{{ t('billing.clientModel') }}</dt>
      <dd class="break-all font-mono">{{ log.model }}</dd>
      <dt>{{ t('billing.finalCharge') }}</dt>
      <dd>{{ formatQuota(log.quota) }}</dd>
    </dl>
    <section
      v-if="discountStatus"
      class="rounded-lg border border-[var(--border-subtle)] p-3 text-sm"
      :aria-label="t('billing.usageDiscount.title')"
      data-log-usage-discount
    >
      <h3 class="mb-2 text-xs font-semibold text-[var(--text-secondary)]">
        {{ t('billing.usageDiscount.title') }}
      </h3>
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5">
        <dt>{{ t('billing.usageDiscount.status') }}</dt>
        <dd>
          {{ t(`billing.usageDiscount.statuses.${discountStatus}`) }}
        </dd>
        <template v-if="discount">
          <dt>{{ t('billing.usageDiscount.baseRatio') }}</dt>
          <dd class="font-mono">{{ discount.base_channel_ratio }}×</dd>
          <dt>{{ t('billing.usageDiscount.factor') }}</dt>
          <dd class="font-mono">{{ discount.factor }}×</dd>
          <dt>{{ t('billing.usageDiscount.appliedRate') }}</dt>
          <dd>{{ percent(discount.applied_rate) }}</dd>
          <dt>{{ t('billing.usageDiscount.components') }}</dt>
          <dd>{{ parts(discount.rates) }}</dd>
          <dt>{{ t('billing.usageDiscount.winner') }}</dt>
          <dd>
            {{
              discount.winner_window
                ? part(discount.winner_window)
                : t('billing.usageDiscount.noWinner')
            }}
          </dd>
          <dt>{{ t('billing.usageDiscount.cap') }}</dt>
          <dd>
            {{
              t(
                discount.cap_applied
                  ? 'billing.usageDiscount.capApplied'
                  : 'billing.usageDiscount.capNotApplied'
              )
            }}
          </dd>
        </template>
      </dl>
      <template v-if="discountAdmin">
        <h4
          class="mb-2 mt-3 text-xs font-semibold text-[var(--text-secondary)]"
        >
          {{ t('billing.usageDiscount.adminTitle') }}
        </h4>
        <dl
          class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5"
          data-log-usage-discount-admin
        >
          <dt>{{ t('billing.usageDiscount.rawRate') }}</dt>
          <dd>{{ percent(discountAdmin.raw_rate) }}</dd>
          <dt>{{ t('billing.usageDiscount.capRate') }}</dt>
          <dd>{{ percent(discountAdmin.cap_rate) }}</dd>
          <dt>{{ t('billing.usageDiscount.configVersion') }}</dt>
          <dd>{{ discountAdmin.config_version }}</dd>
          <dt>{{ t('billing.usageDiscount.shadowRates') }}</dt>
          <dd>
            {{
              Object.keys(discountAdmin.shadow_rates).length
                ? parts(discountAdmin.shadow_rates)
                : t('billing.usageDiscount.noShadow')
            }}
          </dd>
        </dl>
      </template>
    </section>
    <pre
      class="max-h-[60vh] overflow-auto rounded-lg bg-[var(--surface-muted)] p-4 text-xs"
      >{{ JSON.stringify(details, null, 2) }}</pre>
  </div>
</template>
