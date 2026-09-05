<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2, ArrowUp, ArrowDown } from 'lucide-vue-next'
import type { RecordEditorSchema } from '@/constants/settingsEditors'
import editorMessages from '@/i18n/settingsEditor'

const props = defineProps<{ modelValue: string; schema: RecordEditorSchema }>()
const emit = defineEmits<{
  'update:modelValue': [value: string]
  validity: [valid: boolean]
}>()
const { t } = useI18n({ useScope: 'local', messages: editorMessages })
const rows = ref<Record<string, unknown>[]>([])
const raw = ref(false)
const invalid = ref(false)
let lastEmitted = ''

watch(
  () => props.modelValue,
  (value) => {
    if (value === lastEmitted) return
    try {
      const parsed: unknown = JSON.parse(value)
      if (
        !Array.isArray(parsed) ||
        parsed.some(
          (row) => row === null || typeof row !== 'object' || Array.isArray(row)
        )
      )
        throw new Error('Invalid collection')
      rows.value = parsed as Record<string, unknown>[]
      invalid.value = false
    } catch {
      raw.value = true
      invalid.value = true
    }
  },
  { immediate: true }
)

function publish() {
  lastEmitted = JSON.stringify(rows.value)
  invalid.value = false
  emit('update:modelValue', lastEmitted)
  emit('validity', true)
}
function toggleBoolean(row: Record<string, unknown>, key: string) {
  row[key] = row[key] !== true
  publish()
}
function add() {
  const row: Record<string, unknown> = {}
  for (const field of props.schema.fields)
    row[field.key] =
      field.defaultValue ??
      (field.kind === 'datetime'
        ? new Date().toISOString()
        : field.kind === 'boolean'
          ? false
          : '')
  rows.value.push(row)
  publish()
}
function move(index: number, offset: number) {
  const item = rows.value.splice(index, 1)[0]
  rows.value.splice(index + offset, 0, item)
  publish()
}
function remove(index: number) {
  rows.value.splice(index, 1)
  publish()
}
function change(index: number, key: string, kind: string, value: string) {
  if (kind === 'json') {
    try {
      rows.value[index][key] = JSON.parse(value)
    } catch {
      invalid.value = true
      emit('validity', false)
      return
    }
  } else if (kind === 'number')
    rows.value[index][key] =
      value.trim() && Number.isFinite(Number(value)) ? Number(value) : value
  else rows.value[index][key] = value
  publish()
}
</script>

<template>
  <div class="collection-editor">
    <div class="collection-toolbar">
      <div
        role="group"
        :aria-label="t('editor.visual')"
        class="collection-modes"
      >
        <button type="button" :aria-pressed="!raw" @click="raw = false">
          {{ t('editor.visual') }}
        </button>
        <button type="button" :aria-pressed="raw" @click="raw = true">
          {{ t('editor.json') }}
        </button>
      </div>
      <button
        type="button"
        class="collection-icon focus-ring"
        :disabled="!!schema.limit && rows.length >= schema.limit"
        :title="t('editor.add')"
        :aria-label="t('editor.add')"
        @click="add"
      >
        <Plus :size="18" />
      </button>
    </div>
    <textarea
      v-if="raw"
      :value="modelValue"
      class="settings-editor-input font-mono"
      rows="8"
      :aria-label="t('editor.json')"
      @input="
        emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)
      "
    />
    <div v-else class="collection-rows">
      <div v-for="(row, index) in rows" :key="index" class="collection-row">
        <div class="collection-row-tools">
          <span class="font-mono text-xs">{{ index + 1 }}</span>
          <button
            type="button"
            class="collection-icon focus-ring"
            :disabled="index === 0"
            :title="t('editor.up')"
            :aria-label="t('editor.up')"
            @click="move(index, -1)"
          >
            <ArrowUp :size="16" />
          </button>
          <button
            type="button"
            class="collection-icon focus-ring"
            :disabled="index === rows.length - 1"
            :title="t('editor.down')"
            :aria-label="t('editor.down')"
            @click="move(index, 1)"
          >
            <ArrowDown :size="16" />
          </button>
          <button
            type="button"
            class="collection-icon focus-ring"
            :title="t('editor.remove')"
            :aria-label="t('editor.remove')"
            @click="remove(index)"
          >
            <Trash2 :size="16" />
          </button>
        </div>
        <div class="collection-fields">
          <label
            v-for="field in schema.fields"
            :key="field.key"
            class="collection-field"
          >
            <span>{{ t(`editor.fields.${field.key}`) }}</span>
            <button
              v-if="field.kind === 'boolean'"
              type="button"
              class="collection-bool"
              :aria-pressed="row[field.key] === true"
              @click="toggleBoolean(row, field.key)"
            >
              {{ row[field.key] === true ? 'On' : 'Off' }}
            </button>
            <select
              v-else-if="field.choices"
              class="settings-editor-input"
              :value="row[field.key]"
              @change="
                change(
                  index,
                  field.key,
                  field.kind,
                  ($event.target as HTMLSelectElement).value
                )
              "
            >
              <option
                v-for="choice in field.choices"
                :key="choice"
                :value="choice"
              >
                {{ choice }}
              </option>
            </select>
            <textarea
              v-else-if="field.kind === 'json'"
              class="settings-editor-input font-mono"
              rows="3"
              :value="
                JSON.stringify(row[field.key] ?? field.defaultValue, null, 2)
              "
              :aria-invalid="invalid || undefined"
              @input="
                change(
                  index,
                  field.key,
                  field.kind,
                  ($event.target as HTMLTextAreaElement).value
                )
              "
            />
            <input
              v-else
              class="settings-editor-input"
              :type="field.kind === 'number' ? 'number' : 'text'"
              :min="field.min"
              :maxlength="field.maxLength"
              :required="field.required"
              :value="row[field.key]"
              @input="
                change(
                  index,
                  field.key,
                  field.kind,
                  ($event.target as HTMLInputElement).value
                )
              "
            />
          </label>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.collection-toolbar,
.collection-row-tools {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.collection-toolbar {
  justify-content: space-between;
  margin-bottom: 0.75rem;
}
.collection-modes {
  display: flex;
  border: 1px solid var(--outline);
  border-radius: var(--shape-control);
  overflow: hidden;
}
.collection-modes button {
  padding: 0.5rem 1rem;
  font-size: 0.875rem;
}
.collection-modes [aria-pressed='true'] {
  background: var(--accent-soft);
  color: var(--accent-text);
}
.collection-icon {
  width: 44px;
  height: 44px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  border-radius: var(--shape-control);
}
.collection-icon:hover {
  background: var(--state-hover-layer);
}
.collection-icon:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
.collection-row {
  padding: 1rem 0;
  border-top: 1px solid var(--outline-variant);
}
.collection-row-tools {
  justify-content: flex-end;
}
.collection-row-tools span {
  margin-right: auto;
  color: var(--text-tertiary);
}
.collection-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem 1rem;
}
.collection-field {
  display: grid;
  gap: 0.35rem;
  min-width: 0;
  font-size: 0.8125rem;
  color: var(--text-secondary);
}
.settings-editor-input {
  width: 100%;
  min-height: 44px;
  padding: 0.65rem;
  border: 1px solid var(--outline);
  border-radius: var(--shape-control);
  background: var(--surface-container);
  color: var(--text-primary);
}
.collection-bool {
  min-height: 44px;
  border: 1px solid var(--outline);
  border-radius: var(--shape-control);
  color: var(--text-secondary);
}
.collection-bool[aria-pressed='true'] {
  background: var(--accent-soft);
  color: var(--accent-text);
}
@media (max-width: 640px) {
  .collection-fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
