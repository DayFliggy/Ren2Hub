<script setup lang="ts">
// Upstream components/source-diff.tsx: line-level LCS diff.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { diffLines } from '../lib/source-diff'

const props = defineProps<{
  before: string
  after: string
  /** Accessible name of the diff region; defaults to the source diff. */
  label?: string
  /** Text shown when nothing changed; defaults to the source wording. */
  emptyText?: string
}>()
const { t } = useI18n()
const lines = computed(() =>
  props.before === props.after ? [] : diffLines(props.before, props.after)
)
const tone = {
  same: '',
  added: 'bg-[var(--status-success-soft)] text-[var(--status-success-text)]',
  removed: 'bg-[var(--status-danger-soft)] text-[var(--status-danger-text)]',
}
const prefix = { same: ' ', added: '+', removed: '-' }
</script>

<template>
  <p
    v-if="before === after"
    role="status"
    class="rounded-xl border border-[var(--border-subtle)] px-3 py-6 text-center text-sm text-[var(--text-tertiary)]"
  >
    {{ emptyText || t('taskPlugins.noSourceChanges') }}
  </p>
  <div
    v-else
    class="max-h-96 overflow-auto rounded-xl border border-[var(--border-subtle)] font-mono text-xs"
    role="region"
    :aria-label="label || t('taskPlugins.sourceDiff')"
    tabindex="0"
  >
    <!-- Lines share the widest line's width so their tint survives sideways scrolling. -->
    <div class="w-max min-w-full">
      <div
        v-for="line in lines"
        :key="line.id"
        class="whitespace-pre px-3"
        :class="tone[line.kind]"
      >
        {{ prefix[line.kind] }} {{ line.text || ' ' }}
      </div>
    </div>
  </div>
</template>
