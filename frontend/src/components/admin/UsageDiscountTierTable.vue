<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import {
  MAX_TIERS,
  type DraftErrors,
  type TableDraft,
  type TableKey,
  type TierDraft,
} from '@/features/usage-discount/draft'
import { usageDiscountMessages } from '@/i18n/usageDiscount'
import {
  USAGE_DISCOUNT_MODES,
  type UsageDiscountMode,
} from '@/types/usageDiscount'
import './admin.css'

// One tier table of the usage discount draft. The parent owns the draft and
// receives every edit as a whole new table.
const props = defineProps<{
  tableKey: TableKey
  table: TableDraft
  errors: DraftErrors
}>()
const emit = defineEmits<{ change: [table: TableDraft] }>()
const { t } = useI18n({ useScope: 'local', messages: usageDiscountMessages })
const id = useId()

const title = computed(() => t(`table.${props.tableKey}`))
const modeOptions = computed(() =>
  USAGE_DISCOUNT_MODES.map((mode) => ({
    value: mode,
    label: t(`table.modes.${mode}`),
  }))
)

function issue(path: string) {
  const found = props.errors[path]
  return found ? t(found.key, found.params ?? {}) : ''
}
function setMode(mode: string) {
  emit('change', { ...props.table, mode: mode as UsageDiscountMode })
}
function setTier(row: number, patch: Partial<TierDraft>) {
  emit('change', {
    ...props.table,
    tiers: props.table.tiers.map((tier, at) =>
      at === row ? { ...tier, ...patch } : tier
    ),
  })
}
function removeTier(row: number) {
  emit('change', {
    ...props.table,
    tiers: props.table.tiers.filter((_, at) => at !== row),
  })
}
function addTier() {
  emit('change', {
    ...props.table,
    tiers: [...props.table.tiers, { amount: '', percent: '' }],
  })
}
// Errors render only when present; the input points at them by id.
function describedBy(path: string, row: number) {
  return issue(path) ? `${id}-${row}-${path.split('.').at(-1)}` : undefined
}
function text(event: Event) {
  return (event.target as HTMLInputElement).value
}
</script>

<template>
  <section
    class="discount-table"
    :aria-labelledby="`${id}-title`"
    :data-discount-table="tableKey"
  >
    <div class="discount-table-head">
      <div class="min-w-0">
        <h3 :id="`${id}-title`" class="text-sm font-semibold">{{ title }}</h3>
        <p class="admin-muted">{{ t(`table.${tableKey}Hint`) }}</p>
      </div>
      <SegmentedToggle
        :model-value="table.mode"
        :options="modeOptions"
        :label="t('table.mode', { name: title })"
        size="sm"
        @update:model-value="setMode"
      />
    </div>
    <p v-if="table.mode === 'shadow'" class="admin-muted">
      {{ t('table.shadowHint') }}
    </p>

    <p v-if="!table.tiers.length" class="admin-muted">
      {{ t('table.empty') }}
    </p>
    <div
      v-for="(tier, row) in table.tiers"
      :key="row"
      class="discount-tier"
      data-discount-tier
    >
      <label class="admin-field">
        <span>{{ t('table.threshold') }}</span>
        <span class="admin-affix">
          <span class="admin-affix-mark is-start" aria-hidden="true">$</span>
          <input
            :value="tier.amount"
            class="has-start"
            inputmode="decimal"
            :aria-label="t('table.thresholdRow', { row: row + 1 })"
            :aria-invalid="Boolean(issue(`${tableKey}.${row}.amount`))"
            :aria-describedby="describedBy(`${tableKey}.${row}.amount`, row)"
            @input="setTier(row, { amount: text($event) })"
          />
        </span>
        <span
          v-if="issue(`${tableKey}.${row}.amount`)"
          :id="`${id}-${row}-amount`"
          class="admin-error"
          >{{ issue(`${tableKey}.${row}.amount`) }}</span
        >
      </label>
      <label class="admin-field">
        <span>{{ t('table.rate') }}</span>
        <span class="admin-affix">
          <input
            :value="tier.percent"
            class="has-end"
            inputmode="decimal"
            :aria-label="t('table.rateRow', { row: row + 1 })"
            :aria-invalid="Boolean(issue(`${tableKey}.${row}.percent`))"
            :aria-describedby="describedBy(`${tableKey}.${row}.percent`, row)"
            @input="setTier(row, { percent: text($event) })"
          />
          <span class="admin-affix-mark is-end" aria-hidden="true">%</span>
        </span>
        <span
          v-if="issue(`${tableKey}.${row}.percent`)"
          :id="`${id}-${row}-percent`"
          class="admin-error"
          >{{ issue(`${tableKey}.${row}.percent`) }}</span
        >
      </label>
      <ConsoleButton
        variant="ghost"
        size="sm"
        class="discount-tier-remove"
        :aria-label="t('table.removeRow', { row: row + 1 })"
        @click="removeTier(row)"
        >{{ t('table.remove') }}</ConsoleButton
      >
    </div>
    <p v-if="issue(tableKey)" class="admin-error" role="alert">
      {{ issue(tableKey) }}
    </p>
    <div>
      <ConsoleButton
        variant="secondary"
        size="sm"
        :disabled="table.tiers.length >= MAX_TIERS"
        @click="addTier"
        ><Plus :size="13" aria-hidden="true" />{{
          t('table.add')
        }}</ConsoleButton
      >
    </div>
  </section>
</template>

<style scoped>
.discount-table {
  display: grid;
  gap: 0.75rem;
  min-width: 0;
}
.discount-table-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}
.discount-tier {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  align-items: start;
  gap: 0.75rem;
}
.discount-tier-remove {
  /* Aligns with the inputs below their captions. */
  margin-top: 1.5rem;
}
@media (max-width: 480px) {
  .discount-tier {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .discount-tier > :first-child {
    grid-column: 1 / -1;
  }
}
</style>
