<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Save, Trash2 } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { isRecord } from '@/api/contracts'
import { decimalText } from '@/features/pricing/draft'
import { isDecimal } from '@/utils/decimal'
import { modelPricingMessages } from '@/i18n/modelPricing'
const props = defineProps<{ value: string }>()
const emit = defineEmits<{ save: [value: string]; dirty: [value: boolean] }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const mode = ref('visual'),
  rows = ref<Array<{ id: number; key: string; price: string }>>([]),
  json = ref('{}'),
  error = ref('')
let nextId = 1,
  baseline = ''
function parse(text: string) {
  const parsed: unknown = JSON.parse(text)
  if (
    !isRecord(parsed) ||
    Object.entries(parsed).some(
      ([key, value]) =>
        !key.trim() ||
        typeof value !== 'number' ||
        !Number.isFinite(value) ||
        value < 0
    )
  )
    throw new Error(t('toolInvalid'))
  return parsed as Record<string, number>
}
function serialize() {
  const values: Record<string, number> = Object.create(null) as Record<
      string,
      number
    >,
    seen = new Set<string>()
  for (const row of rows.value) {
    const key = row.key.trim()
    if (
      !key ||
      seen.has(key) ||
      !isDecimal(row.price) ||
      !Number.isFinite(Number(row.price))
    )
      throw new Error(t('toolInvalid'))
    seen.add(key)
    values[key] = Number(row.price)
  }
  return JSON.stringify(values, null, 2)
}
watch(
  () => props.value,
  (value) => {
    error.value = ''
    try {
      rows.value = Object.entries(parse(value)).map(([key, price]) => ({
        id: nextId++,
        key,
        price: decimalText(price),
      }))
      json.value = value
      baseline = JSON.stringify([rows.value, json.value])
      emit('dirty', false)
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : String(cause)
    }
  },
  { immediate: true }
)
watch(
  [rows, json],
  () => emit('dirty', JSON.stringify([rows.value, json.value]) !== baseline),
  { deep: true }
)
function toggle() {
  error.value = ''
  try {
    if (mode.value === 'visual') {
      json.value = serialize()
      mode.value = 'json'
    } else {
      rows.value = Object.entries(parse(json.value)).map(([key, price]) => ({
        id: nextId++,
        key,
        price: decimalText(price),
      }))
      mode.value = 'visual'
    }
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
function save() {
  error.value = ''
  try {
    const value =
      mode.value === 'visual'
        ? serialize()
        : JSON.stringify(parse(json.value), null, 2)
    emit('save', value)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
</script>
<template>
  <section class="tool-pricing">
    <div class="tool-header">
      <div>
        <p class="tool-title">{{ t('toolHint') }}</p>
        <p class="text-xs text-[var(--text-tertiary)]">{{ t('noDefault') }}</p>
      </div>
      <div class="tool-actions">
        <ConsoleButton variant="secondary" size="sm" @click="toggle">
          {{ t(mode === 'visual' ? 'json' : 'visual') }}
        </ConsoleButton>
        <ConsoleButton
          v-if="mode === 'visual'"
          size="sm"
          @click="rows.push({ id: nextId++, key: '', price: '' })"
        >
          <Plus :size="13" class="mr-1" aria-hidden="true" />
          {{ t('addPrice') }}
        </ConsoleButton>
      </div>
    </div>

    <div v-if="mode === 'visual'" class="tool-table-wrap">
      <div class="tool-table">
        <table class="w-full min-w-[380px] border-collapse">
          <thead>
            <tr>
              <th
                class="border-b border-[var(--border-subtle)] bg-[var(--surface-table-header,var(--surface-muted))] px-3.5 py-2.5 text-left text-xs font-semibold text-[var(--text-secondary)]"
              >
                {{ t('toolName') }}
              </th>
              <th
                class="border-b border-[var(--border-subtle)] bg-[var(--surface-table-header,var(--surface-muted))] px-3.5 py-2.5 text-left text-xs font-semibold text-[var(--text-secondary)]"
              >
                {{ t('toolPriceUnit') }}
              </th>
              <th
                class="w-20 border-b border-[var(--border-subtle)] bg-[var(--surface-table-header,var(--surface-muted))] px-3.5 py-2.5 text-right text-xs font-semibold text-[var(--text-secondary)]"
              >
                {{ t('actions') }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(row, index) in rows"
              :key="row.id"
              class="border-b border-[var(--border-subtle)] last:border-b-0 transition-colors hover:bg-[var(--surface-hover)]"
            >
              <td class="px-3.5 py-2">
                <label class="block m-0">
                  <span class="sr-only">{{ t('toolName') }}</span>
                  <input
                    v-model="row.key"
                    class="h-11 w-full rounded-lg border border-[var(--border-default)] bg-[var(--surface-solid)] px-2.5 font-mono text-xs text-[var(--text-primary)] transition-colors focus:border-[var(--accent)] focus-ring outline-hidden sm:h-8"
                    :placeholder="t('toolName')"
                  />
                </label>
              </td>
              <td class="px-3.5 py-2">
                <label class="block m-0">
                  <span class="sr-only">{{ t('price') }}</span>
                  <input
                    v-model="row.price"
                    inputmode="decimal"
                    class="h-11 w-full rounded-lg border border-[var(--border-default)] bg-[var(--surface-solid)] px-2.5 font-mono text-xs text-[var(--text-primary)] transition-colors focus:border-[var(--accent)] focus-ring outline-hidden sm:h-8"
                    :placeholder="t('zeroHint')"
                  />
                </label>
              </td>
              <td class="px-3.5 py-2 text-right">
                <ConsoleButton
                  variant="ghost"
                  size="sm"
                  @click="rows.splice(index, 1)"
                >
                  <Trash2
                    :size="13"
                    class="text-[var(--status-danger)]"
                    aria-hidden="true"
                  />
                  <span class="sr-only sm:not-sr-only sm:ml-1">{{
                    t('remove')
                  }}</span>
                </ConsoleButton>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!rows.length" class="tool-empty">
        <p class="text-xs text-[var(--text-tertiary)]">{{ t('empty') }}</p>
      </div>
    </div>

    <label v-else class="block sibling-space-y-1.5">
      <span class="text-xs font-medium text-[var(--text-secondary)]">{{
        t('toolsJson')
      }}</span>
      <textarea
        v-model="json"
        rows="14"
        spellcheck="false"
        class="w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-xs text-[var(--text-primary)] transition-colors focus:border-[var(--accent)] focus-ring outline-hidden"
      />
    </label>

    <p
      v-if="error"
      role="alert"
      class="text-xs text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>

    <div class="tool-footer">
      <ConsoleButton @click="save">
        <Save :size="13" class="mr-1.5" aria-hidden="true" />
        {{ t('toolSave') }}
      </ConsoleButton>
    </div>
  </section>
</template>

<style scoped>
.tool-pricing {
  display: grid;
  gap: 1.25rem;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}
.tool-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  min-width: 0;
}
.tool-title {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}
.tool-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.tool-table-wrap {
  border: 1px solid var(--border-subtle);
  border-radius: 1rem;
  background: var(--surface-solid);
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}
.tool-table {
  min-width: 0;
  max-width: 100%;
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
}
.tool-empty {
  padding: 2rem;
  text-align: center;
}
.tool-footer {
  display: flex;
  justify-content: flex-end;
  border-top: 1px solid var(--border-subtle);
  padding-top: 1rem;
}
</style>
