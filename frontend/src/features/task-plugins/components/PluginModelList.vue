<script setup lang="ts">
// Upstream components/plugin-model-list.tsx. Popover and Collapsible map to
// native <details>, which stays reachable by touch and keyboard.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    models: string[]
    collapsedLabel?: string
    maxVisible?: number
  }>(),
  { collapsedLabel: '', maxVisible: 6 }
)
const { t } = useI18n()
const visible = computed(() => props.models.slice(0, props.maxVisible))
const hidden = computed(() => props.models.slice(props.maxVisible))
const chip =
  'max-w-full rounded-md bg-[var(--surface-muted)] px-1.5 py-0.5 font-mono text-xs break-all text-[var(--text-secondary)]'
</script>

<template>
  <details v-if="collapsedLabel" class="group relative">
    <summary
      class="inline-flex cursor-pointer list-none items-center gap-1 rounded-md border border-[var(--border-subtle)] px-2 py-0.5 text-xs text-[var(--text-secondary)] focus-ring"
    >
      {{ collapsedLabel }}
      <ChevronDown
        :size="12"
        class="transition-transform group-open:rotate-180"
        aria-hidden="true"
      />
    </summary>
    <ul
      class="mt-1.5 max-h-64 sibling-space-y-1 overflow-y-auto font-mono text-xs"
      :aria-label="collapsedLabel"
    >
      <li v-for="model in models" :key="model" class="break-all">
        {{ model }}
      </li>
    </ul>
  </details>
  <div v-else class="min-w-0 sibling-space-y-2">
    <div v-if="visible.length" class="flex flex-wrap gap-1.5">
      <span v-for="model in visible" :key="model" :class="chip">{{
        model
      }}</span>
    </div>
    <details v-if="hidden.length" class="group">
      <summary
        class="inline-flex cursor-pointer list-none items-center gap-1 text-xs text-[var(--text-secondary)] focus-ring"
      >
        {{ t('taskPlugins.moreModels', { count: hidden.length }) }}
        <ChevronDown
          :size="12"
          class="transition-transform group-open:rotate-180"
          aria-hidden="true"
        />
      </summary>
      <div class="flex flex-wrap gap-1.5 pt-2">
        <span v-for="model in hidden" :key="model" :class="chip">{{
          model
        }}</span>
      </div>
    </details>
  </div>
</template>
