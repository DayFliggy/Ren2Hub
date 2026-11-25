<script setup lang="ts">
export interface StatTile {
  label: string
  value: string
  sub?: string
  tone?: 'default' | 'success' | 'warning' | 'danger'
}

withDefaults(
  defineProps<{
    tiles: StatTile[]
    columns?: 2 | 3 | 4
  }>(),
  { columns: 4 }
)

const columnClass = {
  2: 'grid-cols-1 md:grid-cols-2',
  3: 'grid-cols-1 md:grid-cols-2 xl:grid-cols-3',
  4: 'grid-cols-2 xl:grid-cols-4',
} as const

const toneColor = {
  default: 'var(--text-primary)',
  success: 'var(--status-success-text)',
  warning: 'var(--status-warning-text)',
  danger: 'var(--status-danger-text)',
} as const
</script>

<template>
  <div class="grid gap-3 sm:gap-4" :class="columnClass[columns]">
    <div
      v-for="tile in tiles"
      :key="tile.label"
      class="pencil-surface min-w-0 rounded-2xl border bg-[var(--surface-solid)] px-4 py-3"
    >
      <p class="truncate text-xs text-[var(--text-tertiary)]">
        {{ tile.label }}
      </p>
      <p
        class="display-number mt-1 truncate text-xl"
        :style="{ color: toneColor[tile.tone ?? 'default'] }"
      >
        {{ tile.value }}
      </p>
      <p
        v-if="tile.sub"
        class="mt-0.5 truncate text-xs text-[var(--text-tertiary)]"
        :title="tile.sub"
      >
        {{ tile.sub }}
      </p>
    </div>
  </div>
</template>
