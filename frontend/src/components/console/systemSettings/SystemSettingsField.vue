<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import type { SystemSettingValue } from '@/composables/useSystemSettings'
import type { SystemSettingField } from '@/constants/systemSettingsCatalog'
import { SETTINGS_RECORD_EDITORS } from '@/constants/settingsEditors'
import { validateSetting } from '@/utils/settingValidation'
import editorMessages from '@/i18n/settingsEditor'
import SettingsCollectionEditor from './SettingsCollectionEditor.vue'

const props = defineProps<{
  field: SystemSettingField
  modelValue: SystemSettingValue
  secretConfigured?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: SystemSettingValue]
  validity: [valid: boolean]
}>()
const { t } = useI18n({ useScope: 'local', messages: editorMessages })
const schema = computed(() => SETTINGS_RECORD_EDITORS[props.field.key])
const entries = ref<Array<{ key: string; value: string }>>([])
const rawMode = ref(false)
const draftError = ref('')
const map = computed(() =>
  ['key-value', 'ratio', 'discount'].includes(props.field.kind)
)
const structured = computed(
  () => map.value || ['list', 'amount-list'].includes(props.field.kind)
)
const isSecret = computed(() =>
  ['secret', 'secret-textarea'].includes(props.field.kind)
)
const inputType = computed(() =>
  isSecret.value
    ? 'password'
    : props.field.kind === 'number'
      ? 'number'
      : props.field.kind === 'url'
        ? 'url'
        : 'text'
)
const validation = computed(() =>
  validateSetting(props.field, props.modelValue)
)
function syncStructuredEntries(value: SystemSettingValue) {
  if (!structured.value || schema.value) return
  try {
    const parsed: unknown = JSON.parse(String(value))
    if (map.value) {
      if (
        parsed === null ||
        typeof parsed !== 'object' ||
        Array.isArray(parsed)
      )
        throw new Error('Expected an object')
      entries.value = Object.entries(parsed).map(([key, item]) => ({
        key,
        value:
          typeof item === 'string' || typeof item === 'number'
            ? String(item)
            : JSON.stringify(item),
      }))
    } else {
      if (!Array.isArray(parsed)) throw new Error('Expected an array')
      entries.value = parsed.map((item) => ({
        key: '',
        value:
          typeof item === 'string' || typeof item === 'number'
            ? String(item)
            : JSON.stringify(item),
      }))
    }
    rawMode.value = false
    draftError.value = ''
  } catch {
    entries.value = []
    rawMode.value = true
    draftError.value = 'invalidJson'
  }
}
watch(
  [() => props.modelValue, structured, map],
  ([value]) => syncStructuredEntries(value),
  { immediate: true }
)
watch(validation, (value) => emit('validity', value === null), {
  immediate: true,
})

function publish(value: SystemSettingValue) {
  draftError.value = ''
  emit('update:modelValue', value)
  emit('validity', validateSetting(props.field, value) === null)
}
function scalar(value: string) {
  publish(
    props.field.kind === 'number' &&
      value.trim() &&
      Number.isFinite(Number(value))
      ? Number(value)
      : value
  )
}
function publishEntries() {
  draftError.value = ''
  if (map.value) {
    const keys = entries.value.map((entry) => entry.key.trim())
    if (keys.some((key) => !key)) {
      draftError.value = 'required'
      return
    }
    if (new Set(keys).size !== keys.length) {
      draftError.value = 'duplicate'
      return
    }
    const pairs = entries.value.map((entry) => [
      entry.key.trim(),
      props.field.kind === 'key-value'
        ? entry.value
        : entry.value.trim() && Number.isFinite(Number(entry.value))
          ? Number(entry.value)
          : entry.value,
    ])
    publish(JSON.stringify(Object.fromEntries(pairs)))
  } else
    publish(
      JSON.stringify(
        entries.value.map((entry) =>
          props.field.kind === 'amount-list' &&
          entry.value.trim() &&
          Number.isFinite(Number(entry.value))
            ? Number(entry.value)
            : entry.value
        )
      )
    )
}
function add() {
  entries.value.push({ key: '', value: '' })
  publishEntries()
}
function remove(index: number) {
  entries.value.splice(index, 1)
  publishEntries()
}
</script>

<template>
  <div
    class="settings-field-control"
    :class="{
      'settings-field-wide': structured || schema || field.kind === 'json',
      'settings-field-toggle': field.kind === 'boolean',
    }"
  >
    <label
      v-if="field.kind !== 'boolean'"
      :for="`setting-${field.key}`"
      class="settings-field-title"
      >{{ field.label }}</label
    >
    <ConsoleToggle
      v-if="field.kind === 'boolean'"
      :model-value="modelValue === true"
      :label="field.label"
      @update:model-value="publish"
    />
    <SettingsCollectionEditor
      v-else-if="schema"
      :model-value="String(modelValue)"
      :schema="schema"
      @update:model-value="publish"
    />
    <template v-else-if="structured && !rawMode">
      <div
        v-for="(entry, index) in entries"
        :key="index"
        class="settings-structured-row"
        :class="{ 'settings-structured-pair': map }"
      >
        <input
          v-if="map"
          v-model="entry.key"
          class="settings-input"
          :aria-label="`${field.label} ${t('editor.key')} ${index + 1}`"
          :placeholder="t('editor.key')"
          @input="publishEntries"
        />
        <input
          v-model="entry.value"
          class="settings-input"
          :type="
            ['ratio', 'discount', 'amount-list'].includes(field.kind)
              ? 'number'
              : 'text'
          "
          :aria-label="`${field.label} ${t('editor.value')} ${index + 1}`"
          :placeholder="t('editor.value')"
          @input="publishEntries"
        />
        <button
          type="button"
          class="settings-icon focus-ring"
          :title="t('editor.remove')"
          :aria-label="t('editor.remove')"
          @click="remove(index)"
        >
          ×
        </button>
      </div>
      <button
        type="button"
        class="settings-icon focus-ring"
        :title="t('editor.add')"
        :aria-label="t('editor.add')"
        @click="add"
      >
        +
      </button>
    </template>
    <textarea
      v-else-if="
        ['json', 'textarea', 'secret-textarea'].includes(field.kind) || rawMode
      "
      :id="`setting-${field.key}`"
      :value="String(modelValue)"
      :aria-invalid="draftError ? true : undefined"
      class="settings-input settings-textarea"
      :class="{ 'font-mono': field.kind === 'json' || rawMode }"
      rows="5"
      :autocomplete="isSecret ? 'new-password' : 'off'"
      @input="scalar(($event.target as HTMLTextAreaElement).value)"
    />
    <select
      v-else-if="field.options"
      :id="`setting-${field.key}`"
      class="settings-input"
      :value="String(modelValue)"
      :aria-invalid="draftError ? true : undefined"
      @change="scalar(($event.target as HTMLSelectElement).value)"
    >
      <option
        v-for="option in field.options"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>
    <input
      v-else
      :id="`setting-${field.key}`"
      class="settings-input"
      :type="inputType"
      :value="modelValue"
      :min="field.min"
      :max="field.max"
      :step="field.integer ? 1 : 'any'"
      :aria-invalid="draftError ? true : undefined"
      :autocomplete="isSecret ? 'new-password' : 'off'"
      @input="scalar(($event.target as HTMLInputElement).value)"
    />
    <p v-if="draftError || validation" role="alert" class="settings-error">
      {{ t(`editor.${draftError || validation}`) }}
    </p>
    <p v-if="isSecret && secretConfigured" class="settings-hint">
      {{ t('editor.configured') }}
    </p>
  </div>
</template>

<style scoped>
.settings-field-control {
  display: grid;
  gap: 0.5rem;
  min-width: 0;
}
.settings-field-title {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--text-primary);
  overflow-wrap: anywhere;
}
.settings-field-toggle {
  display: flex;
  justify-content: space-between;
  align-items: center;
  min-height: 44px;
  border-bottom: 1px solid var(--outline-variant);
  padding: 0.75rem 0;
}
.settings-input {
  width: 100%;
  min-width: 0;
  min-height: 44px;
  border: 1px solid var(--outline);
  border-radius: var(--shape-control);
  background: var(--surface-container);
  color: var(--text-primary);
  padding: 0.65rem 0.75rem;
  font-size: 0.875rem;
}
.settings-input:focus {
  outline: 2px solid var(--focus-ring);
  outline-offset: 2px;
}
.settings-textarea {
  resize: vertical;
  line-height: 1.5;
}
.settings-structured-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 44px;
  gap: 0.5rem;
}
.settings-structured-pair {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 44px;
}
.settings-icon {
  display: inline-flex;
  width: 44px;
  height: 44px;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--outline-variant);
  border-radius: var(--shape-control);
  font-size: 1.25rem;
}
.settings-icon:hover {
  background: var(--state-hover-layer);
}
.settings-error,
.settings-hint {
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}
.settings-error {
  color: var(--status-danger-text);
}
.settings-hint {
  color: var(--text-secondary);
}
</style>
