<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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
    <p>{{ t('toolHint') }}</p>
    <p class="admin-muted">{{ t('noDefault') }}</p>
    <div class="tool-actions">
      <ConsoleButton variant="secondary" size="sm" @click="toggle">{{
        t(mode === 'visual' ? 'json' : 'visual')
      }}</ConsoleButton
      ><ConsoleButton
        v-if="mode === 'visual'"
        size="sm"
        @click="rows.push({ id: nextId++, key: '', price: '' })"
        >{{ t('addPrice') }}</ConsoleButton
      >
    </div>
    <div v-if="mode === 'visual'" class="tool-table">
      <table class="admin-table">
        <thead>
          <tr>
            <th>{{ t('toolName') }}</th>
            <th>{{ t('price') }} (USD / 1K)</th>
            <th>{{ t('actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, index) in rows" :key="row.id">
            <td>
              <label class="admin-field"
                ><span class="sr-only">{{ t('toolName') }}</span
                ><input v-model="row.key"
              /></label>
            </td>
            <td>
              <label class="admin-field"
                ><span class="sr-only">{{ t('price') }}</span
                ><input
                  v-model="row.price"
                  inputmode="decimal"
                  :placeholder="t('zeroHint')"
              /></label>
            </td>
            <td>
              <ConsoleButton
                variant="ghost"
                size="sm"
                @click="rows.splice(index, 1)"
                >{{ t('remove') }}</ConsoleButton
              >
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!rows.length" class="admin-muted">{{ t('empty') }}</p>
    </div>
    <label v-else class="admin-field"
      ><span>{{ t('tools') }} (JSON)</span><textarea v-model="json" rows="14" />
    </label>
    <p v-if="error" role="alert" class="admin-error">{{ error }}</p>
    <ConsoleButton @click="save">{{ t('toolSave') }}</ConsoleButton>
  </section>
</template>
<style scoped>
.tool-pricing {
  display: grid;
  gap: 1rem;
}
.tool-actions {
  display: flex;
  gap: 0.5rem;
  justify-content: flex-end;
}
.tool-table {
  overflow: auto;
}
.tool-table table {
  min-width: 480px;
}
textarea {
  font-family: monospace;
}
</style>
