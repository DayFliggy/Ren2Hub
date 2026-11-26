<script setup lang="ts">
// Localized from upstream components/marketplace-updates-dialog.tsx for Ren2Hub Vue.
// Rechecks live marketplace index and installed plugin state before sending any
// upload, updates eligible plugins sequentially, and preserves nested modal focus.
import { computed, onBeforeUnmount, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  ChevronDown,
  CircleAlert,
  ListRestart,
} from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import { formatNumber } from '@/utils/format'

import { listTaskPlugins } from '../api'
import {
  fetchMarketplaceIndex,
  getMarketplaceUpdates,
  marketplaceSourceDisplayName,
  marketplaceSourceKind,
  type MarketplaceUpdate,
  type MarketplaceUpdateSkipReason,
} from '../lib/marketplace'
import {
  downloadMarketplaceRelease,
  formatMarketplaceIndexError,
  formatMarketplaceInstallError,
  installMarketplaceRelease,
  MarketplaceActivationError,
} from '../lib/marketplace-install'
import type { MarketplaceSource } from '../types'
import MarketplaceInstallDialog from './MarketplaceInstallDialog.vue'
import MarketplaceUpdateRow, {
  type UpdateResult,
} from './MarketplaceUpdateRow.vue'
import PluginAlert from './PluginAlert.vue'

export type MarketplaceUpdateSelection = {
  source: MarketplaceSource
  updates: MarketplaceUpdate[]
}

const props = defineProps<MarketplaceUpdateSelection>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()

const skippedListId = useId()
const ready = computed(() => props.updates.filter((item) => !item.skip))
const skipped = computed(() => props.updates.filter((item) => item.skip))

const review = ref<MarketplaceUpdate | null>(null)
const busy = ref(false)
const finished = ref(false)
const recheckError = ref('')
const currentKey = ref('')
const results = ref<Map<string, UpdateResult>>(new Map())
const skippedOpen = ref(ready.value.length === 0)
let controller: AbortController | undefined

const sourceDisplayName = computed(() =>
  marketplaceSourceDisplayName(props.source, t('taskPlugins.official'))
)
const sourceKind = computed(() => marketplaceSourceKind(props.source.index_url))
const outcomes = computed(() => [...results.value.values()])
const updatedCount = computed(
  () => outcomes.value.filter((item) => item.status === 'updated').length
)
const skippedAfterReview = computed(
  () => outcomes.value.filter((item) => item.status === 'skipped').length
)
const failedCount = computed(
  () => outcomes.value.length - updatedCount.value - skippedAfterReview.value
)
const progressPercent = computed(() =>
  ready.value.length > 0
    ? Math.min(100, Math.round((results.value.size / ready.value.length) * 100))
    : 0
)

function skipReasonLabel(reason: MarketplaceUpdateSkipReason | 'changed') {
  return t(`taskPlugins.bulkSkipReasons.${reason}`)
}

function requestClose() {
  if (!busy.value) emit('close')
}

async function runUpdates() {
  if (busy.value || finished.value || ready.value.length === 0) return
  controller?.abort()
  const request = new AbortController()
  controller = request
  busy.value = true
  recheckError.value = ''
  currentKey.value = ''
  results.value = new Map()
  let uploadedAny = false

  try {
    // A version may have been withdrawn or disabled while the list was open.
    // Only apply the versions and hashes the administrator actually reviewed.
    const [freshIndex, freshInstalled] = await Promise.all([
      fetchMarketplaceIndex(props.source.index_url, request.signal),
      listTaskPlugins(request.signal),
    ])
    if (request.signal.aborted) return

    const fresh = new Map(
      getMarketplaceUpdates(freshIndex, freshInstalled).map((item) => [
        item.plugin.key,
        item,
      ])
    )

    for (const item of ready.value) {
      if (request.signal.aborted) return
      const key = item.plugin.key
      const next = fresh.get(key)
      let result: UpdateResult
      if (
        !next ||
        next.skip ||
        next.installed.meta.version !== item.installed.meta.version ||
        next.installed.source_hash.toLowerCase() !==
          item.installed.source_hash.toLowerCase() ||
        next.target.version !== item.target.version ||
        next.target.sha256?.toLowerCase() !== item.target.sha256?.toLowerCase()
      ) {
        result = { status: 'skipped', reason: next?.skip ?? 'changed' }
      } else {
        currentKey.value = key
        try {
          const download = await downloadMarketplaceRelease(
            props.source,
            next.plugin,
            next.target,
            request.signal
          )
          const detail = await installMarketplaceRelease(
            props.source,
            next.plugin,
            next.target,
            download,
            {
              signal: request.signal,
              onUploaded: () => {
                uploadedAny = true
              },
            }
          )
          result = { status: 'updated', issues: detail.pricing_issues }
        } catch (error) {
          if (request.signal.aborted) return
          result = {
            status:
              error instanceof MarketplaceActivationError
                ? 'activation-failed'
                : 'failed',
            message: formatMarketplaceInstallError(error, t),
          }
        }
      }
      const nextResults = new Map(results.value)
      nextResults.set(key, result)
      results.value = nextResults
    }
    finished.value = true
    if (uploadedAny) emit('changed')
  } catch (error) {
    if (request.signal.aborted) return
    recheckError.value = formatMarketplaceIndexError(error, t)
  } finally {
    if (!request.signal.aborted) {
      currentKey.value = ''
      busy.value = false
    }
  }
}

onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="
      finished
        ? t('taskPlugins.updateResultsTitle')
        : t('taskPlugins.updateInstalledTitle')
    "
    :subtitle="
      t('taskPlugins.updatesFromSource', { source: sourceDisplayName })
    "
    size="lg"
    :close-disabled="busy"
    @close="requestClose"
  >
    <div class="sibling-space-y-4">
      <p
        v-if="finished"
        role="status"
        class="text-sm font-medium text-[var(--text-primary)]"
      >
        {{
          t('taskPlugins.updateSummary', {
            updated: formatNumber(updatedCount),
            failed: formatNumber(failedCount),
            skipped: formatNumber(skipped.length + skippedAfterReview),
          })
        }}
      </p>

      <div
        v-if="busy"
        role="progressbar"
        :aria-valuenow="results.size"
        aria-valuemin="0"
        :aria-valuemax="ready.length"
        :aria-label="t('taskPlugins.updatingProgress')"
        class="h-2 w-full overflow-hidden rounded-full bg-[var(--surface-muted)]"
      >
        <div
          class="h-full rounded-full bg-[var(--accent)] transition-[width] duration-200"
          :style="{ width: `${progressPercent}%` }"
        />
      </div>

      <PluginAlert v-if="recheckError" tone="danger" role="alert">
        <template #icon><CircleAlert :size="16" /></template>
        {{ recheckError }}
      </PluginAlert>

      <ul
        v-if="ready.length > 0"
        class="sibling-divide-y sibling-divide-[var(--border-subtle)] overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)]"
      >
        <MarketplaceUpdateRow
          v-for="item in ready"
          :key="item.plugin.key"
          :item="item"
          :result="results.get(item.plugin.key)"
          :current="currentKey === item.plugin.key"
          :busy="busy"
          :skip-message="
            results.get(item.plugin.key)?.status === 'skipped'
              ? skipReasonLabel(
                  (
                    results.get(item.plugin.key) as Extract<
                      UpdateResult,
                      { status: 'skipped' }
                    >
                  ).reason
                )
              : undefined
          "
          @review="review = $event"
        />
      </ul>

      <div v-if="skipped.length > 0" class="sibling-space-y-2">
        <button
          type="button"
          class="inline-flex min-h-11 max-w-full items-center gap-1.5 rounded-lg px-2.5 text-sm font-medium text-[var(--text-secondary)] transition-colors focus-ring hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] lg:min-h-8"
          :aria-expanded="skippedOpen"
          :aria-controls="skippedListId"
          @click="skippedOpen = !skippedOpen"
        >
          <ChevronDown
            :size="15"
            aria-hidden="true"
            class="shrink-0 transition-transform duration-150"
            :class="{ 'rotate-180': skippedOpen }"
          />
          <span>
            {{
              t('taskPlugins.skippedCount', {
                total: formatNumber(skipped.length),
              })
            }}
          </span>
        </button>

        <ul
          v-if="skippedOpen"
          :id="skippedListId"
          class="sibling-divide-y sibling-divide-[var(--border-subtle)] rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3.5 text-xs text-[var(--text-secondary)]"
        >
          <li
            v-for="item in skipped"
            :key="item.plugin.key"
            class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 py-2.5"
          >
            <span class="font-mono break-all text-[var(--text-primary)]">
              {{ item.plugin.key }}
            </span>
            <span>{{ item.skip ? skipReasonLabel(item.skip) : '' }}</span>
          </li>
        </ul>
      </div>

      <PluginAlert v-if="!finished && ready.length > 0" tone="warning">
        <template #icon><AlertTriangle :size="16" /></template>
        <div class="sibling-space-y-1">
          <p>{{ t('taskPlugins.reviewWarning') }}</p>
          <p v-if="sourceKind === 'community'">
            {{ t('taskPlugins.communityInstallTelemetry') }}
          </p>
        </div>
      </PluginAlert>
    </div>

    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton
          variant="secondary"
          :disabled="busy"
          @click="requestClose"
        >
          {{
            finished || ready.length === 0
              ? t('common.close')
              : t('common.cancel')
          }}
        </ConsoleButton>
        <ConsoleButton
          v-if="!finished && ready.length > 0"
          :disabled="busy"
          :loading="busy"
          @click="runUpdates"
        >
          <ListRestart v-if="!busy" :size="15" aria-hidden="true" />
          {{
            busy
              ? t('taskPlugins.updatingProgress')
              : t('taskPlugins.updateAllCount', {
                  total: formatNumber(ready.length),
                })
          }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>

  <MarketplaceInstallDialog
    v-if="review"
    :key="`review-${source.index_url}-${review.plugin.key}-${review.target.version}`"
    review-only
    :target="{
      source,
      plugin: review.plugin,
      version: review.target.version,
      installState: {
        status: 'upgradable',
        installedVersion: review.installed.meta.version,
        latestVersion: review.target.version,
      },
    }"
    @close="review = null"
  />
</template>
