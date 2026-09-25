<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { isRecord } from '@/api/contracts'
import type { UsageSchema, UsageField } from '@/api/modelPricing'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
const props = defineProps<{ manifest: unknown; saved?: UsageSchema }>()
const emit = defineEmits<{ apply: [schema: UsageSchema] }>()
const { t } = useI18n()
type ReviewField = {
  name: string
  type: UsageField['type']
  unit: string
  enum: string[]
  source: '' | UsageField['source']
  required: boolean
  integer: boolean
  minimum: string
  maximum: string
}
const fields = ref<ReviewField[]>([])
watch(
  () => props.manifest,
  () => {
    if (!isRecord(props.manifest)) {
      fields.value = []
      return
    }
    fields.value = Object.entries(props.manifest).flatMap(([name, raw]) => {
      if (!isRecord(raw)) return []
      const previous = props.saved?.fields[name]
      const values = Array.isArray(raw.enum)
        ? raw.enum.filter((value): value is string => typeof value === 'string')
        : []
      const type = values.length
        ? 'string'
        : raw.type === 'boolean'
          ? 'boolean'
          : raw.type === 'string'
            ? 'string'
            : 'number'
      return [
        {
          name,
          type,
          enum: values,
          unit: typeof raw.unit === 'string' ? raw.unit : '',
          source: previous?.source ?? '',
          required: previous?.required ?? false,
          integer: previous?.integer ?? false,
          minimum: previous?.minimum ?? '',
          maximum: previous?.maximum ?? '',
        },
      ]
    })
  },
  { immediate: true }
)
const complete = computed(
  () =>
    fields.value.length > 0 &&
    fields.value.every(
      (field) => field.source && (field.type !== 'number' || field.unit)
    )
)
function apply() {
  if (!complete.value) return
  const definitions: Record<string, UsageField> = {}
  for (const field of fields.value) {
    definitions[field.name] = {
      type: field.type,
      source: field.source as UsageField['source'],
      required: field.required,
      ...(field.type === 'number'
        ? {
            unit: field.unit,
            integer: field.integer,
            ...(field.minimum ? { minimum: field.minimum } : {}),
            ...(field.maximum ? { maximum: field.maximum } : {}),
          }
        : {}),
      ...(field.enum.length ? { enum: [...field.enum] } : {}),
    }
  }
  emit('apply', { version: 1, hash: '', fields: definitions })
}
</script>
<template>
  <div class="space-y-3" data-schema-review>
    <p>{{ t('billing.schemaExplicitSources') }}</p>
    <fieldset
      v-for="field in fields"
      :key="field.name"
      class="space-y-2 rounded border border-[var(--border-subtle)] p-3"
    >
      <legend class="break-all px-1 font-mono">
        {{ field.name }} · {{ field.type
        }}{{ field.unit ? ` / ${field.unit}` : '' }}
      </legend>
      <p v-if="field.enum.length" class="break-words">
        {{ field.enum.join(', ') }}
      </p>
      <label class="block"
        >{{ t('billing.factSource')
        }}<select
          v-model="field.source"
          :aria-label="`${field.name} ${t('billing.factSource')}`"
          class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
        >
          <option value="">{{ t('billing.factMissing') }}</option>
          <option
            v-for="source in [
              'request_fixed',
              'completion_fact',
              'estimate_only',
            ]"
            :key="source"
            :value="source"
          >
            {{ t(`billing.factSources.${source}`) }}
          </option>
        </select></label
      >
      <label class="inline-flex items-center gap-2"
        ><input v-model="field.required" type="checkbox" />{{
          t('billing.factRequired')
        }}</label
      >
      <template v-if="field.type === 'number'">
        <label class="ml-4 inline-flex items-center gap-2"
          ><input v-model="field.integer" type="checkbox" />{{
            t('billing.factInteger')
          }}</label
        >
        <div class="grid grid-cols-2 gap-2">
          <label
            >{{ t('billing.factMinimum')
            }}<input
              v-model="field.minimum"
              inputmode="decimal"
              placeholder="0"
              class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2" /></label
          ><label
            >{{ t('billing.factMaximum')
            }}<input
              v-model="field.maximum"
              inputmode="decimal"
              placeholder="2147483647"
              class="mt-1 w-full rounded border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
          /></label>
        </div>
      </template>
    </fieldset>
    <ConsoleButton
      variant="secondary"
      size="sm"
      :disabled="!complete"
      data-apply-schema-review
      @click="apply"
      >{{ t('billing.applyReviewedSchema') }}</ConsoleButton
    >
  </div>
</template>
