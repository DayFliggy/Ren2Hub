<script setup lang="ts">
// Upstream components/usage-schema-table.tsx, with upstream taskEnumLabel /
// taskUsageUnitLabel inlined on Ren2Hub's localizedText.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type {
  BillingUsageFieldSchema,
  BillingUsageSchema,
} from '@/features/pricing/types'
import { localizedText } from '@/utils/localizedText'

const props = defineProps<{ schema: BillingUsageSchema }>()
const { t, locale } = useI18n()
const entries = computed(() =>
  Object.entries(props.schema).sort(([left], [right]) =>
    left.localeCompare(right)
  )
)

function typeLabel(definition: BillingUsageFieldSchema) {
  if (definition.type === 'number') return t('taskPlugins.typeNumber')
  if (definition.type === 'boolean') return t('taskPlugins.typeBoolean')
  return t('taskPlugins.typeEnum')
}

function unitLabel(definition: BillingUsageFieldSchema) {
  const units: Record<string, string> = {
    second: t('taskPlugins.unitSecond'),
    count: t('taskPlugins.unitCount'),
    token: t('taskPlugins.unitToken'),
    credit: t('taskPlugins.unitCredit'),
  }
  const fallback = units[definition.unit ?? ''] ?? '—'
  if (definition.unit !== 'count') return fallback
  return localizedText(definition.unitLabel, locale.value) || fallback
}

function enumValues(definition: BillingUsageFieldSchema) {
  const values = (definition.enum ?? []).map((value) => {
    const label = definition.enumLabels?.[value]
    const localized =
      typeof label === 'object' && label
        ? { ...label, en: label.en?.trim() || value }
        : label
    const text = localizedText(localized, locale.value) || value
    return text === value ? value : `${value} → ${text}`
  })
  return values.join(', ') || '—'
}
</script>

<template>
  <div class="overflow-x-auto rounded-xl border border-[var(--border-subtle)]">
    <table class="w-full min-w-[36rem] text-left text-sm">
      <thead
        class="bg-[var(--surface-table-header)] text-xs text-[var(--text-tertiary)]"
      >
        <tr>
          <th class="px-3 py-2 font-medium">{{ t('taskPlugins.name') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('taskPlugins.type') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('taskPlugins.unit') }}</th>
          <th class="px-3 py-2 font-medium">
            {{ t('taskPlugins.enumValues') }}
          </th>
          <th class="px-3 py-2 font-medium">
            {{ t('taskPlugins.description') }}
          </th>
        </tr>
      </thead>
      <tbody class="divide-y divide-[var(--border-subtle)]">
        <tr v-for="[name, definition] in entries" :key="name">
          <td class="max-w-64 break-words px-3 py-2 font-mono text-xs">
            {{ name }}
          </td>
          <td class="px-3 py-2">{{ typeLabel(definition) }}</td>
          <td class="px-3 py-2">{{ unitLabel(definition) }}</td>
          <td class="max-w-64 break-words px-3 py-2 font-mono text-xs">
            {{ enumValues(definition) }}
          </td>
          <td class="min-w-48 break-words px-3 py-2">
            {{ localizedText(definition.description, locale) || '—' }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
