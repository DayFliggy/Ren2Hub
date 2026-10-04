<script setup lang="ts">
// Upstream components/plugin-metadata-card.tsx.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { channelTypeLabel } from '../lib/channel-type'
import { getPluginWebsite } from '../lib/plugin-website'
import type { TaskPluginMeta } from '../types'
import CopyButton from './CopyButton.vue'
import PluginEndpoints from './PluginEndpoints.vue'
import PluginModelList from './PluginModelList.vue'
import PluginWebsiteLink from './PluginWebsiteLink.vue'

const props = defineProps<{ meta: TaskPluginMeta }>()
const { t } = useI18n()
const models = computed(() => props.meta.models ?? [])
const fields = computed(() => [
  {
    label: t('taskPlugins.sortPriority'),
    value: String(props.meta.sortPriority ?? 0),
  },
  { label: t('taskPlugins.fetchMode'), value: props.meta.fetchMode ?? '' },
  {
    label: t('taskPlugins.channelTypes'),
    value: props.meta.channelTypes?.length
      ? props.meta.channelTypes
          .map(
            (type) =>
              `${channelTypeLabel(type, t('taskPlugins.taskPlugin'))} (#${type})`
          )
          .join(', ')
      : t('taskPlugins.taskPlugin'),
  },
])
const canCopyBaseUrl = computed(() => {
  try {
    const url = new URL(props.meta.baseUrl ?? '')
    return url.protocol === 'https:' || url.protocol === 'http:'
  } catch {
    // A missing or invalid URL is shown verbatim without a copy action.
    return false
  }
})
const section = 'rounded-xl border border-[var(--border-subtle)] p-4'
</script>

<template>
  <div class="min-w-0 space-y-4">
    <section :class="section" :aria-label="t('taskPlugins.pluginMetadata')">
      <h3 class="mb-4 text-sm font-semibold">
        {{ t('taskPlugins.pluginMetadata') }}
      </h3>
      <dl class="grid gap-x-6 gap-y-4 sm:grid-cols-2">
        <div
          v-for="field in fields"
          :key="field.label"
          class="min-w-0 space-y-1"
        >
          <dt class="text-xs text-[var(--text-tertiary)]">{{ field.label }}</dt>
          <dd class="break-words text-sm">
            {{ field.value || t('taskPlugins.notDeclared') }}
          </dd>
        </div>
        <div class="min-w-0 space-y-1">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('taskPlugins.pluginWebsite') }}
          </dt>
          <dd>
            <PluginWebsiteLink
              v-if="getPluginWebsite(meta.website)"
              :website="meta.website"
            />
            <span v-else class="text-sm">{{
              t('taskPlugins.notDeclared')
            }}</span>
          </dd>
        </div>
        <div class="min-w-0 space-y-1 sm:col-span-2">
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('taskPlugins.defaultBaseUrl') }}
          </dt>
          <dd class="flex items-start gap-2">
            <span class="min-w-0 flex-1 break-all pt-1 font-mono text-xs">{{
              meta.baseUrl || t('taskPlugins.notDeclared')
            }}</span>
            <CopyButton
              v-if="canCopyBaseUrl"
              :value="meta.baseUrl ?? ''"
              :label="t('taskPlugins.copyBaseUrl')"
            />
          </dd>
        </div>
      </dl>
    </section>
    <section :class="section" :aria-label="t('taskPlugins.models')">
      <div class="mb-3 flex items-center gap-2">
        <h3 class="text-sm font-semibold">{{ t('taskPlugins.models') }}</h3>
        <span
          class="rounded-md bg-[var(--surface-muted)] px-1.5 text-xs text-[var(--text-secondary)]"
          >{{ models.length }}</span
        >
        <CopyButton
          v-if="models.length"
          class="ml-auto"
          :value="models.join('\n')"
          :label="t('taskPlugins.copyAllModels')"
        />
      </div>
      <PluginModelList
        v-if="models.length"
        :models="models"
        :max-visible="models.length"
      />
      <p v-else class="text-sm text-[var(--text-tertiary)]">
        {{ t('taskPlugins.notDeclared') }}
      </p>
    </section>
    <section :class="section" :aria-label="t('taskPlugins.endpoints')">
      <PluginEndpoints :protocols="meta.protocols" :routes="meta.routes" />
    </section>
  </div>
</template>
