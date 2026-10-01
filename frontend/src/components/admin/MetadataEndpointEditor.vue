<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2 } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import './admin.css'
import {
  endpointMethods,
  parseMetadataEndpoints,
  serializeMetadataEndpoints,
  type MetadataEndpointRow,
} from './modelMetadataForm'

const props = defineProps<{ modelValue: string; disabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n({ useScope: 'local', messages: modelMetadataMessages })
const mode = ref<'table' | 'json'>('table')
const rows = ref<MetadataEndpointRow[]>([])
const source = ref('')
const error = ref('')
let inferredSource: string | null = null
let initialRows = ''
let initialSource = ''
watch(
  () => props.modelValue,
  (value) => {
    source.value = value
    initialSource = value
    try {
      rows.value = parseMetadataEndpoints(value)
      inferredSource = value.trim().startsWith('[') ? value : null
      initialRows = JSON.stringify(rows.value)
      error.value = ''
    } catch (cause) {
      mode.value = 'json'
      error.value = t((cause as Error).message)
    }
  },
  { immediate: true }
)
function serializeTable(): string {
  // Unedited inferred types must not become explicit endpoint overrides.
  if (inferredSource !== null && JSON.stringify(rows.value) === initialRows)
    return inferredSource
  return serializeMetadataEndpoints(rows.value)
}
function switchMode(value: string) {
  if (mode.value === value) return
  try {
    if (value === 'table') rows.value = parseMetadataEndpoints(source.value)
    else source.value = serializeTable()
    mode.value = value as 'table' | 'json'
    error.value = ''
  } catch (cause) {
    error.value = t((cause as Error).message)
  }
}
/** Unsaved edits that have not been written back through validate(). */
function isDirty(): boolean {
  return mode.value === 'json'
    ? source.value !== initialSource
    : JSON.stringify(rows.value) !== initialRows
}
function validate(): string {
  try {
    let value: string
    if (mode.value === 'json') {
      const parsed = parseMetadataEndpoints(source.value)
      value = source.value.trim().startsWith('[')
        ? source.value
        : serializeMetadataEndpoints(parsed)
    } else value = serializeTable()
    error.value = ''
    emit('update:modelValue', value)
    return value
  } catch (cause) {
    error.value = t((cause as Error).message)
    throw new Error(error.value, { cause })
  }
}
defineExpose({ validate, isDirty })
</script>
<template>
  <div class="space-y-3">
    <SegmentedToggle
      :model-value="mode"
      :options="[
        { value: 'table', label: t('endpointTable') },
        { value: 'json', label: t('endpointJson') },
      ]"
      :label="t('endpointConfiguration')"
      size="sm"
      @update:model-value="switchMode"
    />
    <label v-if="mode === 'json'" class="admin-field"
      ><span class="sr-only">{{ t('endpointJson') }}</span
      ><textarea
        v-model="source"
        :disabled="disabled"
        spellcheck="false"
        class="endpoint-source"
      />
    </label>
    <div v-else class="space-y-2">
      <p
        v-if="!rows.length"
        class="rounded-xl border border-dashed border-[var(--border-subtle)] px-4 py-5 text-center text-xs text-[var(--text-tertiary)]"
      >
        {{ t('endpointsEmpty') }}
      </p>
      <div v-for="(row, index) in rows" :key="index" class="endpoint-row">
        <label class="admin-field"
          ><span>{{ t('endpointType') }}</span
          ><input v-model="row.type" :disabled="disabled"
        /></label>
        <label class="admin-field endpoint-path"
          ><span>{{ t('endpointPath') }}</span
          ><input v-model="row.path" :disabled="disabled"
        /></label>
        <label class="admin-field"
          ><span>{{ t('endpointMethod') }}</span
          ><select v-model="row.method" :disabled="disabled">
            <option v-for="method in endpointMethods" :key="method">
              {{ method }}
            </option>
          </select></label
        >
        <IconButton
          :label="t('endpointRemove')"
          :disabled="disabled"
          tone="danger"
          @click="rows.splice(index, 1)"
          ><Trash2 :size="16"
        /></IconButton>
      </div>
      <ConsoleButton
        variant="ghost"
        size="sm"
        :disabled="disabled"
        @click="rows.push({ type: '', path: '', method: 'POST' })"
        ><Plus :size="16" />{{ t('endpointAdd') }}</ConsoleButton
      >
    </div>
    <p class="text-xs text-[var(--text-tertiary)]">
      {{ t('endpointsHint') }}
    </p>
    <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
  </div>
</template>
<style scoped>
.endpoint-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 2fr) 6.5rem 2rem;
  align-items: end;
  gap: 0.5rem;
}
.endpoint-source {
  min-height: 14rem;
  font-family: monospace;
}
@media (max-width: 640px) {
  .endpoint-row {
    grid-template-columns: minmax(0, 1fr) 6.5rem 2rem;
  }
  .endpoint-path {
    grid-column: 1 / -1;
    grid-row: 2;
  }
}
</style>
