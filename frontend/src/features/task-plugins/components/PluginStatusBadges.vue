<script setup lang="ts">
// The source and runtime cells of upstream plugins-table.tsx. Both the table
// and the card view render them, as upstream does through flexRender.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import StatusChip from '@/components/common/StatusChip.vue'

import { isStaleFactoryOverride } from '../lib/marketplace'
import type { TaskPluginListItem } from '../types'

const props = defineProps<{
  plugin: TaskPluginListItem
  part: 'source' | 'runtime'
}>()
const { t } = useI18n()

const staleHint = computed(() =>
  isStaleFactoryOverride(props.plugin)
    ? t('taskPlugins.staleFactory', {
        factory: props.plugin.factory_meta?.version,
      })
    : ''
)
const runtime = computed(() => {
  const status = props.plugin.runtime_status
  if (status === 'registered')
    return { tone: 'neutral' as const, label: t('taskPlugins.registered') }
  if (status === 'compile_failed')
    return { tone: 'danger' as const, label: t('taskPlugins.compileFailed') }
  if (status === 'disabled')
    return { tone: 'neutral' as const, label: t('taskPlugins.disabled') }
  if (status === 'disabled_fallback')
    return {
      tone: 'neutral' as const,
      label: props.plugin.factory_meta
        ? t('taskPlugins.disabledFallback')
        : t('taskPlugins.disabledUnavailable'),
    }
  return { tone: 'neutral' as const, label: t('taskPlugins.notRegistered') }
})
</script>

<template>
  <template v-if="part === 'source'">
    <StatusChip v-if="plugin.source === 'factory'" tone="neutral">{{
      t('taskPlugins.factory')
    }}</StatusChip>
    <span
      v-else-if="plugin.source === 'override_over_factory'"
      class="flex min-w-0 flex-col items-start gap-0.5"
      :title="staleHint || undefined"
    >
      <StatusChip tone="accent">{{
        t('taskPlugins.customOverrides', {
          version: plugin.factory_meta?.version,
        })
      }}</StatusChip>
      <span v-if="staleHint" class="text-xs text-[var(--text-tertiary)]">{{
        staleHint
      }}</span>
    </span>
    <StatusChip v-else tone="accent">{{
      t('taskPlugins.thirdParty')
    }}</StatusChip>
  </template>
  <StatusChip
    v-else
    :tone="runtime.tone"
    :title="plugin.runtime_error || undefined"
    >{{ runtime.label }}</StatusChip
  >
  <template v-if="part === 'runtime'">
    <StatusChip v-if="plugin.task !== false" tone="neutral">{{
      t('pluginExtensions.task')
    }}</StatusChip>
    <StatusChip v-if="plugin.provider" tone="neutral">{{
      t('pluginExtensions.provider')
    }}</StatusChip>
    <StatusChip v-if="plugin.transform" tone="neutral">{{
      t('pluginExtensions.transform')
    }}</StatusChip>
  </template>
  <p
    v-if="part === 'runtime' && plugin.runtime_error"
    class="mt-1 max-w-full break-words text-xs text-[var(--status-danger-text)]"
  >
    {{ plugin.runtime_error }}
  </p>
</template>
