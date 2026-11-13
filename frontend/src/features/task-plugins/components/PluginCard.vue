<script setup lang="ts">
// Upstream components/plugin-card.tsx: identity, source, runtime, version,
// bound models and the enable toggle. Manifest detail stays in the sheet so
// every card keeps the same height.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { localizedText } from '@/utils/localizedText'

import type { TaskPluginListItem } from '../types'
import PluginIcon from './PluginIcon.vue'
import PluginStatusBadges from './PluginStatusBadges.vue'
import PluginWebsiteLink from './PluginWebsiteLink.vue'

const MAX_VISIBLE_MODELS = 4

const props = defineProps<{
  plugin: TaskPluginListItem
  menuItems: ActionMenuItem[]
  busy: boolean
}>()
const emit = defineEmits<{
  select: [key: string]
  toggle: [enabled: boolean]
}>()
const { t, locale } = useI18n()
const description = computed(() =>
  localizedText(props.plugin.meta.description, locale.value)
)
const models = computed(() => props.plugin.meta.models ?? [])
const hiddenModels = computed(() => models.value.slice(MAX_VISIBLE_MODELS))
const label = 'select-none text-[11px] font-medium text-[var(--text-tertiary)]'
</script>

<template>
  <article
    class="flex h-full min-w-0 flex-col gap-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
    data-task-plugin-card
  >
    <div class="flex items-start justify-between gap-2">
      <div class="flex min-w-0 flex-1 items-center gap-2.5">
        <PluginIcon
          class="mt-0.5"
          :plugin="{ ...plugin.meta, hasIcon: plugin.has_icon }"
          :size="20"
        />
        <div class="min-w-0">
          <div class="truncate text-sm font-medium">
            {{
              localizedText(plugin.meta.name_i18n, locale) || plugin.meta.name
            }}
          </div>
          <div class="truncate font-mono text-xs text-[var(--text-tertiary)]">
            {{ plugin.meta.key }}
          </div>
        </div>
      </div>
      <ActionMenu
        :items="menuItems"
        :label="t('taskPlugins.openMenu')"
        @select="emit('select', $event)"
      />
    </div>
    <PluginWebsiteLink :website="plugin.meta.website" />
    <div class="flex flex-wrap items-center gap-1.5">
      <PluginStatusBadges :plugin="plugin" part="source" />
      <PluginStatusBadges :plugin="plugin" part="runtime" />
      <StatusChip
        tone="neutral"
        class="font-mono"
        :aria-label="`${t('taskPlugins.activeVersion')} ${plugin.meta.version}`"
        >{{ plugin.meta.version ? `v${plugin.meta.version}` : '—' }}</StatusChip
      >
    </div>
    <p
      v-if="description"
      class="line-clamp-2 text-xs text-[var(--text-secondary)]"
    >
      {{ description }}
    </p>
    <div v-if="models.length" class="sibling-space-y-1.5">
      <div :class="label">{{ t('taskPlugins.models') }}</div>
      <div class="flex flex-wrap gap-1">
        <span
          v-for="model in models.slice(0, MAX_VISIBLE_MODELS)"
          :key="model"
          class="max-w-full truncate rounded-md border border-[var(--border-subtle)] px-1.5 py-0.5 font-mono text-xs"
          :title="model"
          >{{ model }}</span
        >
        <span
          v-if="hiddenModels.length"
          class="rounded-md bg-[var(--surface-muted)] px-1.5 py-0.5 text-xs"
          :title="hiddenModels.join(', ')"
          >+{{ hiddenModels.length }}</span
        >
      </div>
    </div>
    <div
      class="mt-auto flex items-center justify-between gap-2 border-t border-[var(--border-subtle)] pt-2"
    >
      <span :class="label">{{ t('taskPlugins.enabled') }}</span>
      <ConsoleToggle
        :model-value="
          plugin.enabled && plugin.runtime_status !== 'compile_failed'
        "
        size="sm"
        :disabled="busy || plugin.runtime_status === 'compile_failed'"
        :label="t('taskPlugins.enablePluginAria', { key: plugin.meta.key })"
        @update:model-value="emit('toggle', $event)"
      />
    </div>
  </article>
</template>
