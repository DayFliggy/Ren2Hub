<script setup lang="ts">
// Localized from upstream components/marketplace-update-row.tsx for Ren2Hub Vue.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, CheckCircle2, CircleAlert, Loader2 } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { localizedText } from '@/utils/localizedText'

import type {
  MarketplaceUpdate,
  MarketplaceUpdateSkipReason,
} from '../lib/marketplace'
import type { TaskPluginDetail } from '../types'
import PluginIcon from './PluginIcon.vue'
import PricingIssueList from './PricingIssueList.vue'

export type UpdateResult =
  | { status: 'updated'; issues: TaskPluginDetail['pricing_issues'] }
  | { status: 'failed' | 'activation-failed'; message: string }
  | { status: 'skipped'; reason: MarketplaceUpdateSkipReason | 'changed' }

const props = withDefaults(
  defineProps<{
    item: MarketplaceUpdate
    result?: UpdateResult
    current: boolean
    busy: boolean
    skipMessage?: string
  }>(),
  { result: undefined, skipMessage: '' }
)
const emit = defineEmits<{ review: [item: MarketplaceUpdate] }>()
const { t, locale } = useI18n()

const displayName = computed(
  () =>
    localizedText(props.item.plugin.name_i18n, locale.value) ||
    props.item.plugin.name
)
</script>

<template>
  <li class="sibling-space-y-2 px-3.5 py-3">
    <div
      class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1.5 sm:grid-cols-[minmax(0,1fr)_auto_7.5rem]"
    >
      <div class="flex min-w-0 items-center gap-2.5">
        <PluginIcon
          :plugin="{
            ...item.plugin,
            hasIcon: item.installed.has_icon,
          }"
          :size="28"
        />
        <div class="min-w-0">
          <p class="break-words text-sm font-medium text-[var(--text-primary)]">
            {{ displayName }}
          </p>
          <p
            class="truncate font-mono text-xs text-[var(--text-tertiary)]"
            :title="item.plugin.key"
          >
            {{ item.plugin.key }}
          </p>
        </div>
      </div>

      <div
        class="col-start-1 row-start-2 flex flex-wrap items-center gap-1.5 pl-9 font-mono text-xs break-all sm:col-start-2 sm:row-start-1 sm:pl-0"
      >
        <span class="text-[var(--text-tertiary)]">
          v{{ item.installed.meta.version }}
        </span>
        <ArrowRight
          :size="12"
          aria-hidden="true"
          class="shrink-0 text-[var(--text-tertiary)]"
        />
        <span
          class="rounded bg-[var(--surface-muted)] px-1.5 py-0.5 text-[var(--text-primary)]"
        >
          v{{ item.target.version }}
        </span>
      </div>

      <div
        class="col-start-2 row-start-1 justify-self-end text-xs sm:col-start-3"
      >
        <span
          v-if="result?.status === 'updated'"
          class="inline-flex items-center gap-1.5 font-medium text-[var(--status-success-text)]"
        >
          <CheckCircle2 :size="14" aria-hidden="true" />
          {{ t('taskPlugins.updatedSuccessfully') }}
        </span>
        <span
          v-else-if="result?.status === 'skipped'"
          class="inline-flex items-center gap-1.5 text-[var(--text-tertiary)]"
        >
          {{ t('taskPlugins.skipped') }}
        </span>
        <span
          v-else-if="result"
          class="inline-flex items-center gap-1.5 font-medium text-[var(--status-danger-text)]"
        >
          <CircleAlert :size="14" aria-hidden="true" />
          {{ t('taskPlugins.failed') }}
        </span>
        <span
          v-else-if="busy && current"
          class="inline-flex items-center gap-1.5 text-[var(--text-primary)]"
        >
          <Loader2 :size="14" class="animate-spin" aria-hidden="true" />
          {{ t('taskPlugins.updating') }}
        </span>
        <span
          v-else-if="busy"
          class="inline-flex items-center gap-1.5 text-[var(--text-tertiary)]"
        >
          {{ t('taskPlugins.queued') }}
        </span>
        <ConsoleButton
          v-else
          variant="ghost"
          size="sm"
          :aria-label="t('taskPlugins.reviewPluginAria', { name: displayName })"
          @click="emit('review', item)"
        >
          {{ t('taskPlugins.reviewChanges') }}
        </ConsoleButton>
      </div>
    </div>

    <p
      v-if="result?.status === 'skipped' && skipMessage"
      class="text-xs text-[var(--text-tertiary)]"
    >
      {{ skipMessage }}
    </p>
    <div
      v-if="
        result?.status === 'failed' || result?.status === 'activation-failed'
      "
      class="sibling-space-y-0.5 break-words text-xs text-[var(--status-danger-text)]"
    >
      <p v-if="result.status === 'activation-failed'" class="font-medium">
        {{ t('taskPlugins.uploadedActivationFailed') }}
      </p>
      <p>{{ result.message }}</p>
    </div>
    <PricingIssueList
      v-if="result?.status === 'updated' && result.issues?.length"
      :issues="result.issues"
    />
  </li>
</template>
