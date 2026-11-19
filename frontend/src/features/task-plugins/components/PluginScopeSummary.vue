<script setup lang="ts">
// Upstream components/plugin-scope-summary.tsx: the models and channel types
// a plugin is made for. A side the scope leaves open reads as all models or
// all channels.
import { useI18n } from 'vue-i18n'

import { channelTypeLabel } from '../lib/channel-type'
import type { RequestPluginScope } from '../types'

defineProps<{ scope: RequestPluginScope }>()
const { t } = useI18n()
</script>

<template>
  <dl class="sibling-space-y-1.5 text-sm" data-plugin-scope>
    <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <dt class="shrink-0 text-[var(--text-tertiary)]">
        {{ t('pluginExtensions.rp.scopeChannelsLabel') }}
      </dt>
      <dd v-if="scope.channel_types.length === 0">
        {{ t('pluginExtensions.rp.scopeAllChannels') }}
      </dd>
      <dd v-for="type in scope.channel_types" v-else :key="type">
        {{ channelTypeLabel(type, t('taskPlugins.taskPlugin')) }}
      </dd>
    </div>
    <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <dt class="shrink-0 text-[var(--text-tertiary)]">
        {{ t('pluginExtensions.rp.models') }}
      </dt>
      <dd v-if="scope.models.length === 0">
        {{ t('pluginExtensions.rp.modelsAll') }}
      </dd>
      <dd
        v-for="model in scope.models"
        v-else
        :key="model"
        class="break-all font-mono text-xs"
      >
        {{ model }}
      </dd>
    </div>
  </dl>
</template>
