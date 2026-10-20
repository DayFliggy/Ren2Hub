<script setup lang="ts" generic="T extends number | null">
import { useId } from 'vue'
import { useI18n } from 'vue-i18n'
const model = defineModel<T>({ required: true })
defineProps<{
  label: string
  max: number
  min?: number
  nullable?: boolean
  disabled?: boolean
}>()
const { t } = useI18n()
const id = useId()
</script>

<template>
  <div class="min-w-0 sibling-space-y-1.5">
    <label
      :for="id"
      class="block text-sm font-medium text-[var(--text-secondary)]"
      >{{ label }}</label
    >
    <label
      v-if="nullable"
      class="flex items-center gap-2 text-xs text-[var(--text-secondary)]"
    >
      <input
        type="checkbox"
        :checked="model === null"
        :disabled="disabled"
        class="accent-[var(--accent)]"
        @change="
          model = (($event.target as HTMLInputElement).checked ? null : 0) as T
        "
      />
      {{ t('gateway.inherit') }}
    </label>
    <input
      :id="id"
      :value="model ?? ''"
      type="number"
      :min="min ?? 0"
      :max="max"
      step="1"
      :disabled="disabled || (nullable && model === null)"
      :placeholder="nullable && model === null ? t('gateway.inherit') : ''"
      class="h-10 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-sm text-[var(--text-primary)] focus-ring disabled:opacity-50"
      @input="model = ($event.target as HTMLInputElement).valueAsNumber as T"
    />
  </div>
</template>
