<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2 } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import {
  endpointTemplates,
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
const template = ref('')
let inferredSource: string | null = null
let initialRows = ''
watch(
  () => props.modelValue,
  (value) => {
    source.value = value
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
function switchMode(value: 'table' | 'json') {
  if (mode.value === value) return
  try {
    if (value === 'table') rows.value = parseMetadataEndpoints(source.value)
    else source.value = serializeTable()
    mode.value = value
    error.value = ''
  } catch (cause) {
    error.value = t((cause as Error).message)
  }
}
function addTemplate() {
  if (!template.value) return
  try {
    const current =
      mode.value === 'json'
        ? parseMetadataEndpoints(source.value)
        : [...rows.value]
    if (current.some((row) => row.type === template.value))
      throw new Error('endpointTemplateDuplicate')
    current.push({ type: template.value, ...endpointTemplates[template.value] })
    rows.value = current
    source.value = serializeMetadataEndpoints(current)
    error.value = ''
    template.value = ''
  } catch (cause) {
    error.value = t((cause as Error).message)
  }
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
defineExpose({ validate })
</script>
<template>
  <div class="admin-form">
    <div class="endpoint-toolbar">
      <div class="endpoint-modes" role="group" :aria-label="t('endpoints')">
        <button
          v-for="item in ['table', 'json'] as const"
          :key="item"
          type="button"
          :aria-pressed="mode === item"
          :disabled="disabled"
          @click="switchMode(item)"
        >
          {{ t(item === 'table' ? 'endpointTable' : 'endpointJson') }}
        </button>
      </div>
      <label class="admin-field endpoint-template"
        ><span class="sr-only">{{ t('endpointTemplate') }}</span
        ><select v-model="template" :disabled="disabled" @change="addTemplate">
          <option value="">{{ t('endpointTemplate') }}</option>
          <option
            v-for="(_, name) in endpointTemplates"
            :key="name"
            :value="name"
          >
            {{ name }}
          </option>
        </select></label
      >
    </div>
    <label v-if="mode === 'json'" class="admin-field"
      ><span class="sr-only">{{ t('endpointJson') }}</span
      ><textarea
        v-model="source"
        :disabled="disabled"
        spellcheck="false"
        class="endpoint-source"
      />
    </label>
    <div v-else class="admin-form">
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
          :label="t('remove')"
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
    <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
  </div>
</template>
<style scoped>
.endpoint-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  align-items: center;
}
.endpoint-modes {
  display: flex;
  border: 1px solid var(--border-default);
  border-radius: var(--shape-small);
  overflow: hidden;
}
.endpoint-modes button {
  min-height: 2.5rem;
  padding: 0.5rem 1rem;
  color: var(--text-secondary);
}
.endpoint-modes button[aria-pressed='true'] {
  background: var(--accent-soft);
  color: var(--accent-text);
}
.endpoint-modes button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.endpoint-template {
  flex: 1;
  min-width: 12rem;
}
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
