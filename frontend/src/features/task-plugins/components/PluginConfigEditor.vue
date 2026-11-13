<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import FormField from '@/components/common/FormField.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import type {
  RequestPluginConfigSchema,
  RequestPluginConfigField,
} from '../types'
import { localizedText } from '@/utils/localizedText'
const emit = defineEmits<{ validity: [valid: boolean] }>()
const props = defineProps<{ schema?: RequestPluginConfigSchema }>()
const model = defineModel<Record<string, unknown>>({ required: true })
const { t, locale } = useI18n()
const raw = ref('')
const errors = ref<Record<string, string>>({})
watch(
  model,
  (value) => {
    raw.value = JSON.stringify(value, null, 2)
  },
  { immediate: true, deep: true }
)
watch(errors, (value) => emit('validity', Object.keys(value).length === 0), {
  deep: true,
  immediate: true,
})
const fields = computed(() =>
  Object.entries(props.schema?.properties ?? {}).filter(
    ([, field]) =>
      !field.visibleWhen ||
      Object.entries(field.visibleWhen).every(([name, values]) =>
        values.includes(
          (model.value[name] ?? props.schema?.properties[name]?.default) as
            string | number | boolean
        )
      )
  )
)
function update(name: string, field: RequestPluginConfigField, event: Event) {
  const value = (event.target as HTMLInputElement).value
  try {
    let next: unknown = value
    if (value === '' && !props.schema?.required?.includes(name)) {
      const nextModel = { ...model.value }
      delete nextModel[name]
      model.value = nextModel
      delete errors.value[name]
      return
    }
    if (field.type === 'number' || field.type === 'integer') {
      next = Number(value)
      if (
        !value ||
        !Number.isFinite(next) ||
        (field.type === 'integer' && !Number.isInteger(next))
      )
        throw new Error(t('pluginExtensions.invalidConfig'))
    }
    if (field.type === 'array' || field.type === 'object') {
      next = JSON.parse(value)
      if (
        field.type === 'array'
          ? !Array.isArray(next)
          : !next || typeof next !== 'object' || Array.isArray(next)
      )
        throw new Error(t('pluginExtensions.invalidConfig'))
    }
    model.value = { ...model.value, [name]: next }
    delete errors.value[name]
  } catch {
    errors.value[name] = t('pluginExtensions.invalidConfig')
  }
}
function updateRaw() {
  try {
    const parsed: unknown = JSON.parse(raw.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
      throw new Error()
    model.value = parsed as Record<string, unknown>
    delete errors.value.raw
  } catch {
    errors.value.raw = t('pluginExtensions.invalidConfig')
  }
}
function fieldValue(name: string, field: RequestPluginConfigField) {
  const value = model.value[name] ?? field.default
  return value === undefined
    ? ''
    : typeof value === 'object'
      ? JSON.stringify(value, null, 2)
      : String(value)
}
</script>
<template>
  <div class="grid gap-3 md:grid-cols-2">
    <template v-if="schema">
      <FormField
        v-for="[name, field] in fields"
        :key="name"
        :label="localizedText(field.title, locale) || name"
        :class="
          ['array', 'object'].includes(field.type) ||
          field.format === 'textarea' ||
          field.format === 'keywords'
            ? 'md:col-span-2'
            : ''
        "
      >
        <ConsoleToggle
          v-if="field.type === 'boolean'"
          :model-value="Boolean(model[name] ?? field.default)"
          :label="localizedText(field.title, locale) || name"
          @update:model-value="model = { ...model, [name]: $event }"
        />
        <select
          v-else-if="field.enum"
          :value="fieldValue(name, field)"
          class="config-input"
          @change="update(name, field, $event)"
        >
          <option
            v-for="(value, index) in field.enum"
            :key="value"
            :value="value"
          >
            {{ localizedText(field.enumTitles?.[index], locale) || value }}
          </option>
        </select>
        <textarea
          v-else-if="
            ['array', 'object'].includes(field.type) ||
            ['textarea', 'keywords', 'json'].includes(field.format ?? '')
          "
          :value="fieldValue(name, field)"
          class="config-input"
          rows="4"
          :required="schema.required?.includes(name)"
          @change="update(name, field, $event)"
        />
        <input
          v-else
          :value="fieldValue(name, field)"
          :type="
            ['number', 'integer'].includes(field.type)
              ? 'number'
              : field.format === 'password'
                ? 'password'
                : 'text'
          "
          :min="field.minimum"
          :max="field.maximum"
          :step="field.type === 'integer' ? 1 : 'any'"
          :minlength="field.minLength"
          :maxlength="field.maxLength"
          :pattern="field.pattern"
          :required="schema.required?.includes(name)"
          class="config-input"
          @change="update(name, field, $event)"
        />
        <p
          v-if="field.description"
          class="mt-1 text-xs text-[var(--text-secondary)]"
        >
          {{ localizedText(field.description, locale) }}
        </p>
        <p
          v-if="errors[name]"
          role="alert"
          class="text-xs text-[var(--status-danger)]"
        >
          {{ errors[name] }}
        </p>
      </FormField>
    </template>
    <FormField
      v-else
      :label="t('pluginExtensions.config')"
      class="md:col-span-2"
    >
      <textarea
        v-model="raw"
        class="config-input font-mono"
        rows="5"
        @change="updateRaw"
      />
      <p v-if="errors.raw" role="alert">{{ errors.raw }}</p></FormField
    >
  </div>
</template>
<style scoped>
.config-input {
  width: 100%;
  padding: 0.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-muted);
  color: var(--text-primary);
}
</style>
