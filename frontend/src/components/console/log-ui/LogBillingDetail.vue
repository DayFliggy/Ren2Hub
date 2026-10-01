<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseBillingCharge } from '@/api/billing'
import { logEvidence, logModelObservation } from '@/utils/logModel'
import LogModelMismatch from './LogModelMismatch.vue'
import BillingAdjustments from './BillingAdjustments.vue'
import type { LogItem } from '@/types/console'
import { formatDecimal, formatDecimalUSD } from '@/utils/decimal'

const props = defineProps<{ log: LogItem }>()
const { t } = useI18n()
const evidence = computed(() => logEvidence(props.log))
const observation = computed(() => logModelObservation(props.log))
const chargeState = computed(() => {
  const saved = evidence.value.charge_result
  if (saved === null || saved === undefined)
    return { charge: null, invalid: false }
  try {
    return {
      charge: parseBillingCharge(saved, 'saved log charge'),
      invalid: false,
    }
  } catch {
    return { charge: null, invalid: true }
  }
})
const charge = computed(() => chargeState.value.charge)
const upstreamModel = computed(() => observation.value.upstream)
</script>

<template>
  <div class="space-y-4 text-left" data-log-billing-detail>
    <dl class="grid grid-cols-[auto_1fr] gap-2 text-sm">
      <dt>{{ t('billing.clientModel') }}</dt>
      <dd class="break-all font-mono">{{ log.model }}</dd>
      <template v-if="upstreamModel && upstreamModel !== log.model"
        ><dt>{{ t('billing.upstreamModel') }}</dt>
        <dd class="break-all font-mono">{{ upstreamModel }}</dd></template
      >
      <template v-if="observation.response"
        ><dt>{{ t('billing.responseModel') }}</dt>
        <dd class="break-all font-mono">
          {{ observation.response }}
        </dd></template
      >
      <template v-if="evidence.requested_service_tier"
        ><dt>{{ t('billing.requestedServiceTier') }}</dt>
        <dd>{{ evidence.requested_service_tier }}</dd></template
      >
      <template v-if="evidence.actual_service_tier"
        ><dt>{{ t('billing.actualServiceTier') }}</dt>
        <dd>{{ evidence.actual_service_tier }}</dd></template
      >
      <dt>{{ t('billing.priceTitle') }}</dt>
      <dd>{{ evidence.price_version_id ?? '—' }}</dd>
      <dt>{{ t('billing.priceScope') }}</dt>
      <dd>
        {{ evidence.price_scope ?? '—'
        }}<span v-if="evidence.price_channel_id">
          · {{ evidence.price_channel_id }}</span
        >
      </dd>
      <dt>{{ t('billing.quoteHash') }}</dt>
      <dd class="break-all font-mono">{{ evidence.quote_hash ?? '—' }}</dd>
      <dt>{{ t('billing.priceVersionHash') }}</dt>
      <dd class="break-all font-mono">
        {{ evidence.price_version_hash ?? '—' }}
      </dd>
    </dl>
    <LogModelMismatch :log="log" />
    <p
      v-if="!charge"
      class="text-sm"
      :class="
        chargeState.invalid
          ? 'text-[var(--status-danger-text)]'
          : 'text-[var(--text-secondary)]'
      "
    >
      {{
        t(
          chargeState.invalid
            ? 'billing.chargeInvalid'
            : 'billing.chargePending'
        )
      }}
    </p>
    <template v-else>
      <BillingAdjustments :charge="charge" />
      <p class="text-sm">
        {{ t('billing.finalCharge') }}:
        {{ formatDecimalUSD(charge.total_usd) }} ·
        {{ charge.final_quota }} quota
      </p>
      <div class="overflow-x-auto">
        <table class="w-full text-left text-xs">
          <thead>
            <tr>
              <th>{{ t('billing.quantity') }}</th>
              <th>{{ t('billing.unitPrice') }}</th>
              <th>{{ t('billing.finalCharge') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(line, index) in charge.lines"
              :key="index"
              class="border-t border-[var(--border-subtle)]"
            >
              <td class="py-2">
                {{ line.quantity }} × {{ formatDecimal(line.count) }}
              </td>
              <td>{{ formatDecimalUSD(line.unit_price) }} / {{ line.unit }}</td>
              <td>{{ formatDecimalUSD(line.subtotal_usd) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="charge.trace" class="space-y-2 text-xs">
        <p v-if="charge.trace.image_count !== undefined">
          {{ t('billing.quantities.images') }}: {{ charge.trace.image_count }}
        </p>
        <p>
          {{ t('billing.matchedTier') }}: {{ charge.trace.matched_tier || '—' }}
        </p>
        <p
          v-for="(rule, index) in charge.trace.request_rules"
          :key="index"
          class="break-words rounded p-2"
          :class="
            rule.matched
              ? 'bg-[var(--accent-muted)]'
              : 'text-[var(--text-tertiary)]'
          "
        >
          {{ rule.cond }} · × {{ rule.multiplier }} ·
          {{
            t(rule.matched ? 'billing.ruleMatched' : 'billing.ruleUnmatched')
          }}
        </p>
      </div>
      <p class="text-xs text-[var(--text-tertiary)]">
        {{ t('billing.frozenChargeHint') }} · {{ charge.calculation_version }}
      </p>
    </template>
  </div>
</template>
