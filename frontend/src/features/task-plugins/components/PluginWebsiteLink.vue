<script setup lang="ts">
// Upstream components/plugin-website-link.tsx (hugeicons LinkSquare01 maps to
// lucide SquareArrowOutUpRight).
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { SquareArrowOutUpRight } from 'lucide-vue-next'

import { getPluginWebsite } from '../lib/plugin-website'

const props = withDefaults(defineProps<{ website?: string }>(), {
  website: undefined,
})
const { t } = useI18n()
const href = computed(() => getPluginWebsite(props.website))
</script>

<template>
  <a
    v-if="href"
    :href="href"
    target="_blank"
    rel="noopener noreferrer"
    class="inline-flex max-w-full items-center gap-1 text-xs text-[var(--accent-text)] hover:underline focus-ring"
  >
    <SquareArrowOutUpRight :size="12" class="shrink-0" aria-hidden="true" />
    {{ t('taskPlugins.pluginWebsite') }}
  </a>
</template>
