<script setup lang="ts">
// Upstream request-plugin-picker.tsx: the first step of adding an instance,
// one card per plugin.
import { useI18n } from 'vue-i18n'

import { localizedText } from '@/utils/localizedText'
import type { TaskPluginOption } from '../types'
import PluginIcon from './PluginIcon.vue'
import RequestPluginRunLine from './RequestPluginRunLine.vue'

defineProps<{
  plugins: TaskPluginOption[]
  /** How many instances each plugin already has, by plugin key. */
  instanceCounts: Record<string, number>
}>()
const emit = defineEmits<{ pick: [key: string] }>()
const { t, locale } = useI18n()
</script>

<template>
  <p
    v-if="!plugins.length"
    class="py-10 text-center text-sm text-[var(--text-tertiary)]"
  >
    {{ t('pluginExtensions.rp.noPlugins') }}
  </p>
  <div v-else class="grid gap-3 sm:grid-cols-2">
    <button
      v-for="plugin in plugins"
      :key="plugin.key"
      type="button"
      class="flex min-w-0 items-start gap-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4 text-left transition-colors hover:border-[var(--border-strong)] hover:bg-[var(--state-hover-layer)] focus-ring"
      :data-plugin-card="plugin.key"
      @click="emit('pick', plugin.key)"
    >
      <span
        aria-hidden="true"
        class="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-[var(--surface-muted)]"
      >
        <PluginIcon :plugin="plugin" :size="24" />
      </span>
      <span class="flex min-w-0 flex-1 flex-col gap-1">
        <span class="flex items-baseline justify-between gap-2">
          <span
            class="min-w-0 truncate text-sm font-semibold text-[var(--text-primary)]"
            >{{ localizedText(plugin.name_i18n, locale) || plugin.name }}</span
          >
          <span
            v-if="instanceCounts[plugin.key]"
            class="shrink-0 text-xs text-[var(--text-tertiary)]"
            >{{
              t('pluginExtensions.rp.added', {
                count: instanceCounts[plugin.key],
              })
            }}</span
          >
        </span>
        <span class="line-clamp-2 text-[13px] text-[var(--text-secondary)]">{{
          localizedText(plugin.description, locale)
        }}</span>
        <RequestPluginRunLine :plugin="plugin" class="pt-1.5" />
      </span>
    </button>
  </div>
</template>
