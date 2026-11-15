<script setup lang="ts">
// Upstream request-plugin-summary.tsx RequestPluginRunLine: when a request
// processing plugin runs, and what a failure of it does.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { TaskPluginOption } from '../types'
import {
  requestPluginFailure,
  requestPluginStageKey,
} from '../lib/request-plugins'

const props = defineProps<{ plugin: TaskPluginOption }>()
const { t } = useI18n()

const stages = computed(() =>
  (props.plugin.transform_hooks ?? []).map((hook) => {
    const key = requestPluginStageKey(hook)
    return key ? t(key) : hook
  })
)
const failure = computed(() => requestPluginFailure(props.plugin))
</script>

<template>
  <span
    class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs font-normal text-[var(--text-tertiary)]"
  >
    <template v-for="(stage, index) in stages" :key="stage">
      <span>{{ stage }}</span>
      <span v-if="failure || index < stages.length - 1" aria-hidden="true"
        >·</span
      >
    </template>
    <span v-if="failure" class="inline-flex items-center gap-1.5">
      <span
        aria-hidden="true"
        class="h-1.5 w-1.5 rounded-full"
        :class="
          failure === 'skipped'
            ? 'bg-[var(--status-success)]'
            : 'bg-[var(--status-warning)]'
        "
      />
      {{ t(`pluginExtensions.rp.failure.${failure}`) }}
    </span>
  </span>
</template>
