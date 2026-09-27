<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { BillingCharge } from '@/api/billing'
import { formatDecimal } from '@/utils/decimal'
defineProps<{ charge: BillingCharge }>()
const { t } = useI18n()
</script>
<template>
  <dl class="space-y-2 text-xs" data-billing-adjustments>
    <div class="flex flex-wrap justify-between gap-2">
      <dt>{{ t('billing.baseCharge') }}</dt>
      <dd class="font-mono">{{ formatDecimal(charge.base_usd) }} USD</dd>
    </div>
    <div
      v-for="(adjustment, index) in charge.adjustments"
      :key="index"
      class="flex flex-wrap justify-between gap-2"
    >
      <dt>
        {{ t('billing.channelAdjustment') }} ×
        {{ formatDecimal(adjustment.multiplier) }}
      </dt>
      <dd class="font-mono">{{ formatDecimal(adjustment.amount_usd) }} USD</dd>
    </div>
    <div class="flex flex-wrap justify-between gap-2">
      <dt>{{ t('billing.finalCharge') }}</dt>
      <dd class="font-mono">{{ formatDecimal(charge.total_usd) }} USD</dd>
    </div>
  </dl>
</template>
