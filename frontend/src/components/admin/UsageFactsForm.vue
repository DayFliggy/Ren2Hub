<script setup lang="ts">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UsageSchema } from '@/api/modelPricing'
const props = defineProps<{
  schema: UsageSchema
  modelValue: Record<string, unknown>
  sources: string[]
}>()
const emit = defineEmits<{
  'update:modelValue': [value: Record<string, unknown>]
}>()
const { t } = useI18n()
const id = useId()
function update(name: string, value: string) {
  const next = { ...props.modelValue }
  if (value === '') delete next[name]
  else
    next[name] =
      props.schema.fields[name]?.type === 'boolean' ? value === 'true' : value
  emit('update:modelValue', next)
}
function text(value: unknown) {
  return value === undefined || value === null ? '' : String(value)
}
</script>
<template>
  <div class="grid gap-3 sm:grid-cols-2" data-usage-facts>
    <label
      v-for="[name, field] in Object.entries(schema.fields).filter(
        ([, field]) => sources.includes(field.source)
      )"
      :key="name"
      class="min-w-0 text-xs"
    >
      <span class="block break-all font-mono"
        >{{ name }}{{ field.required ? ' *' : '' }}</span
      >
      <span class="mb-1 block text-[var(--text-tertiary)]"
        >{{ t(`billing.factSources.${field.source}`)
        }}{{ field.unit ? ` · ${field.unit}` : '' }}</span
      >
      <select
        v-if="field.type === 'boolean' || field.enum?.length"
        :id="`${id}-${name}`"
        :aria-label="name"
        :value="text(modelValue[name])"
        class="w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 focus-ring"
        @change="update(name, ($event.target as HTMLSelectElement).value)"
      >
        <option value="">{{ t('billing.factMissing') }}</option>
        <template v-if="field.type === 'boolean'"
          ><option value="true">true</option>
          <option value="false">false</option></template
        >
        <template v-else>
          <option
            v-if="
              text(modelValue[name]) &&
              !field.enum?.includes(text(modelValue[name]))
            "
            :value="text(modelValue[name])"
          >
            {{ text(modelValue[name]) }} ({{ t('billing.factLegacyValue') }})
          </option>
          <option v-for="value in field.enum" :key="value" :value="value">
            {{ value }}
          </option>
        </template>
      </select>
      <input
        v-else
        :id="`${id}-${name}`"
        :aria-label="name"
        :value="text(modelValue[name])"
        :inputmode="field.type === 'number' ? 'decimal' : undefined"
        :placeholder="t('billing.factMissing')"
        class="w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2 font-mono focus-ring"
        @input="update(name, ($event.target as HTMLInputElement).value)"
      />
      <span
        v-if="
          field.minimum !== undefined ||
          field.maximum !== undefined ||
          field.integer
        "
        class="mt-1 block text-[var(--text-tertiary)]"
        >{{ field.minimum ?? '0' }} – {{ field.maximum ?? '2147483647'
        }}{{ field.integer ? ` · ${t('billing.factInteger')}` : '' }}</span
      >
    </label>
  </div>
</template>
