<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useId } from 'vue'
import type { ConversionPolicy } from '@/types/gateway'
const model = defineModel<ConversionPolicy>({ required: true })
defineProps<{ disabled?: boolean }>()
const { t } = useI18n()
const id = useId()
</script>
<template>
  <div class="min-w-0 space-y-1.5">
    <label
      :for="id"
      class="block text-sm font-medium text-[var(--text-secondary)]"
      >{{ t('gateway.conversionPolicy') }}</label
    >
    <select
      :id="id"
      v-model="model"
      name="conversion-policy"
      :disabled="disabled"
      class="h-10 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-sm text-[var(--text-primary)] focus-ring"
    >
      <option
        v-for="policy in ['safe', 'allow', 'strict'] as const"
        :key="policy"
        :value="policy"
      >
        {{ t(`gateway.conversion.${policy}`) }}
      </option>
    </select>
  </div>
</template>
