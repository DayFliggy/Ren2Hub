<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2 } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import {
  MAX_TIERS,
  type DraftErrors,
  type TableDraft,
  type TableKey,
  type TierDraft,
} from '@/features/topup-discount/draft'
import { topupDiscountMessages } from '@/i18n/topupDiscount'
import {
  TOPUP_DISCOUNT_MODES,
  type TopupDiscountMode,
} from '@/types/topupDiscount'
import './admin.css'

// One tier table of the top-up discount draft. The parent owns the draft and
// receives every edit as a whole new table.
const props = defineProps<{
  tableKey: TableKey
  table: TableDraft
  errors: DraftErrors
}>()
const emit = defineEmits<{ change: [table: TableDraft] }>()
const { t } = useI18n({ useScope: 'local', messages: topupDiscountMessages })
const id = useId()

const title = computed(() => t(`table.${props.tableKey}`))
const modeOptions = computed(() =>
  TOPUP_DISCOUNT_MODES.map((mode) => ({
    value: mode,
    label: t(`table.modes.${mode}`),
  }))
)

function issue(path: string) {
  const found = props.errors[path]
  return found ? t(found.key, found.params ?? {}) : ''
}
function setMode(mode: string) {
  emit('change', { ...props.table, mode: mode as TopupDiscountMode })
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
    :data-discount-table="tableKey"
    :aria-labelledby="`${id}-title`"
  >
    <div class="discount-table-head">
      <div>
        <h3 :id="`${id}-title`">{{ title }}</h3>
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
    <p v-if="table.mode === 'shadow'" class="discount-mode-note">
      {{ t('table.shadowHint') }}
    </p>
    <p v-else-if="table.mode === 'off'" class="admin-muted">
      {{ t('table.offHint') }}
    </p>
    <p v-if="!table.tiers.length" class="discount-table-empty admin-muted">
      {{ t('table.empty') }}
    </p>
    <table v-else class="discount-tiers" :aria-label="title">
      <thead>
        <tr>
          <th scope="col">{{ t('table.threshold') }}</th>
          <th scope="col">{{ t('table.rate') }}</th>
          <th scope="col">
            <span class="sr-only">{{ t('table.remove') }}</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(tier, row) in table.tiers" :key="row" data-discount-tier>
          <td>
            <div class="admin-field">
              <span class="admin-affix">
                <span class="admin-affix-mark is-start" aria-hidden="true"
                  >$</span
                >
                <input
                  :value="tier.amount"
                  class="has-start"
                  inputmode="decimal"
                  :aria-label="t('table.thresholdRow', { row: row + 1 })"
                  :aria-invalid="Boolean(issue(`${tableKey}.${row}.amount`))"
                  :aria-describedby="
                    describedBy(`${tableKey}.${row}.amount`, row)
                  "
                  @input="setTier(row, { amount: text($event) })"
                />
              </span>
              <span
                v-if="issue(`${tableKey}.${row}.amount`)"
                :id="`${id}-${row}-amount`"
                class="admin-error"
                >{{ issue(`${tableKey}.${row}.amount`) }}</span
              >
            </div>
          </td>
          <td>
            <div class="admin-field">
              <span class="admin-affix">
                <input
                  :value="tier.percent"
                  class="has-end"
                  inputmode="decimal"
                  :aria-label="t('table.rateRow', { row: row + 1 })"
                  :aria-invalid="Boolean(issue(`${tableKey}.${row}.percent`))"
                  :aria-describedby="
                    describedBy(`${tableKey}.${row}.percent`, row)
                  "
                  @input="setTier(row, { percent: text($event) })"
                />
                <span class="admin-affix-mark is-end" aria-hidden="true"
                  >%</span
                >
              </span>
              <span
                v-if="issue(`${tableKey}.${row}.percent`)"
                :id="`${id}-${row}-percent`"
                class="admin-error"
                >{{ issue(`${tableKey}.${row}.percent`) }}</span
              >
            </div>
          </td>
          <td>
            <ConsoleButton
              variant="ghost"
              size="sm"
              class="discount-tier-remove"
              :aria-label="t('table.removeRow', { row: row + 1 })"
              @click="removeTier(row)"
            >
              <Trash2 :size="15" aria-hidden="true" />
            </ConsoleButton>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="issue(tableKey)" class="admin-error" role="alert">
      {{ issue(tableKey) }}
    </p>
    <div class="discount-table-footer">
      <ConsoleButton
        variant="secondary"
        size="sm"
        :disabled="table.tiers.length >= MAX_TIERS"
        @click="addTier"
        ><Plus :size="14" aria-hidden="true" />{{
          t('table.add')
        }}</ConsoleButton
      >
      <span class="admin-muted">{{
        t('table.tierCount', { count: table.tiers.length, max: MAX_TIERS })
      }}</span>
    </div>
  </section>
</template>

<style scoped>
.discount-table {
  display: grid;
  gap: 0.875rem;
  min-width: 0;
}
.discount-table-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.875rem;
}
.discount-table-head > :first-child {
  flex: 1;
  min-width: min(100%, 15rem);
}
.discount-table-head h3 {
  margin-bottom: 0.25rem;
  font-size: 0.9375rem;
  font-weight: 650;
}
.discount-table-head > [role='radiogroup'] {
  flex-shrink: 0;
}
.discount-mode-note {
  padding: 0.625rem 0.75rem;
  border-radius: 0.625rem;
  background: var(--accent-soft);
  color: var(--accent-text);
  font-size: 0.75rem;
  line-height: 1.6;
}
.discount-table-empty {
  padding: 1rem;
  text-align: center;
  border: 1px dashed var(--border-default);
  border-radius: 0.625rem;
}
.discount-tiers {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
}
.discount-tiers th {
  padding: 0 0.625rem 0.375rem 0;
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--text-secondary);
  text-align: left;
}
.discount-tiers td {
  padding: 0.25rem 0.625rem 0.25rem 0;
  vertical-align: top;
}
.discount-tiers th:nth-child(2) {
  width: 32%;
}
.discount-tiers th:last-child {
  width: 2.75rem;
}
.discount-tiers td:last-child,
.discount-tiers th:last-child {
  padding-right: 0;
}
.discount-tiers input {
  min-height: 2.75rem;
  font-variant-numeric: tabular-nums;
}
.discount-tier-remove {
  width: 2.75rem;
  min-height: 2.75rem;
  padding: 0;
  color: var(--text-secondary);
}
.discount-tier-remove:hover {
  color: var(--status-danger-text);
  background: var(--status-danger-soft);
}
.discount-table-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}
</style>
