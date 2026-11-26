<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import type { ModelPricingIssue } from '@/api/pricingConfig'

defineProps<{ issues: ModelPricingIssue[] }>()
const { t } = useI18n()
</script>

<template>
  <div class="sibling-space-y-1 text-xs text-[var(--status-warning-text)]">
    <p class="font-medium">{{ t('taskPlugins.pricingIssuesHint') }}</p>
    <ul class="list-disc space-y-0.5 pl-4">
      <li
        v-for="issue in issues"
        :key="`${issue.plugin_key}-${issue.model_name}-${issue.reason}`"
        class="break-words"
      >
        <span class="font-mono">{{ issue.model_name }}</span>
        ({{ issue.plugin_name || issue.plugin_key }}):
        {{ t(`taskPlugins.pricingIssueReasons.${issue.reason}`) }}
      </li>
    </ul>
  </div>
</template>
