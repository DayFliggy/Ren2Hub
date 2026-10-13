<script setup lang="ts">
// Upstream components/marketplace-panel.tsx. Only the selected source is
// requested; alternates stay idle until the administrator switches.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw, Settings2, TriangleAlert } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'

import { listMarketplaceSources } from '../api'
import {
  DEFAULT_MARKETPLACE_INDEX_URL,
  deriveInstallState,
  fetchMarketplaceIndex,
  indexHasIntegrityHashes,
  isDefaultMarketplaceSource,
} from '../lib/marketplace'
import { PluginSourceFetchError } from '../lib/plugin-url'
import type {
  MarketplaceIndex,
  MarketplaceSource,
  TaskPluginListItem,
} from '../types'
import MarketplaceInstallDialog, {
  type MarketplaceInstallTarget,
} from './MarketplaceInstallDialog.vue'
import MarketplacePluginCard from './MarketplacePluginCard.vue'
import MarketplaceSourcesDialog from './MarketplaceSourcesDialog.vue'
import PluginAlert from './PluginAlert.vue'

const props = defineProps<{ installed: TaskPluginListItem[] }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()

const sources = ref<MarketplaceSource[] | null>(null)
const sourcesError = ref('')
const selectedUrl = ref('')
const index = ref<MarketplaceIndex | null>(null)
const indexLoading = ref(false)
const indexError = ref('')
const sourcesOpen = ref(false)
const sourcesLoading = ref(false)
const installTarget = ref<MarketplaceInstallTarget | null>(null)
let controller: AbortController | undefined

const selected = computed(
  () =>
    sources.value?.find((source) => source.index_url === selectedUrl.value) ??
    sources.value?.[0]
)
const sourceOptions = computed(() =>
  (sources.value ?? []).map((source) => ({
    value: source.index_url,
    // The built-in source names come from the server as English identifiers.
    // Keep custom names editable while presenting the defaults in the active
    // console language.
    label:
      source.index_url === DEFAULT_MARKETPLACE_INDEX_URL
        ? t('taskPlugins.official')
        : source.name,
  }))
)
const missingHashes = computed(() =>
  index.value ? !indexHasIntegrityHashes(index.value) : false
)

async function loadSources() {
  sourcesError.value = ''
  sourcesLoading.value = true
  try {
    sources.value = await listMarketplaceSources()
  } catch (error) {
    // Keep the source list unavailable so the management dialog cannot turn
    // a transient read failure into an empty replacement on save.
    sources.value = null
    sourcesError.value = error instanceof Error ? error.message : String(error)
  } finally {
    sourcesLoading.value = false
  }
}

async function openSources() {
  await loadSources()
  if (!sourcesError.value && sources.value !== null) sourcesOpen.value = true
}

async function loadIndex() {
  controller?.abort()
  index.value = null
  indexError.value = ''
  const source = selected.value
  if (!source) return
  const request = new AbortController()
  controller = request
  indexLoading.value = true
  try {
    const result = await fetchMarketplaceIndex(source.index_url, request.signal)
    if (!request.signal.aborted) index.value = result
  } catch (error) {
    if (request.signal.aborted) return
    indexError.value =
      error instanceof PluginSourceFetchError && error.reason === 'not_found'
        ? t('taskPlugins.indexHttp', { status: error.status ?? 0 })
        : error instanceof Error
          ? error.message
          : String(error)
  } finally {
    if (!request.signal.aborted) indexLoading.value = false
  }
}

watch(() => selected.value?.index_url, loadIndex)
void loadSources()
onBeforeUnmount(() => controller?.abort())

function sourcesSaved(saved: MarketplaceSource[]) {
  sources.value = saved
  sourcesOpen.value = false
  void loadIndex()
}

function onInstalled() {
  installTarget.value = null
  emit('changed')
}
</script>

<template>
  <div class="sibling-space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <p class="text-sm text-[var(--text-secondary)]">
        {{ t('taskPlugins.marketHint') }}
      </p>
      <div class="flex shrink-0 gap-2">
        <ConsoleButton
          variant="secondary"
          size="sm"
          :disabled="!selected || indexLoading"
          @click="loadIndex"
        >
          <RefreshCw :size="14" aria-hidden="true" />
          {{ t('taskPlugins.refresh') }}
        </ConsoleButton>
        <ConsoleButton
          variant="secondary"
          size="sm"
          :disabled="sourcesLoading"
          @click="openSources"
        >
          <Settings2 :size="14" aria-hidden="true" />
          {{ t('taskPlugins.manageSources') }}
        </ConsoleButton>
      </div>
    </div>
    <SkeletonBlock v-if="sources === null" rounded="xl" class="h-24" />
    <p
      v-if="sourcesError"
      role="alert"
      class="text-sm text-[var(--status-danger-text)]"
    >
      {{ sourcesError }}
    </p>
    <div
      v-if="sources !== null && !sources.length"
      class="sibling-space-y-3 text-center"
    >
      <EmptyState
        :title="t('taskPlugins.noSources')"
        :hint="t('taskPlugins.addIndexHint')"
      />
      <ConsoleButton :disabled="sourcesLoading" @click="openSources">
        {{ t('taskPlugins.manageSources') }}
      </ConsoleButton>
    </div>
    <div
      v-if="(sources?.length ?? 0) > 1 && selected"
      class="max-w-full overflow-x-auto"
    >
      <SegmentedToggle
        :model-value="selected.index_url"
        :options="sourceOptions"
        :label="t('taskPlugins.marketplaceSources')"
        size="sm"
        @update:model-value="selectedUrl = $event"
      />
    </div>
    <section v-if="selected" class="sibling-space-y-3" data-marketplace-source>
      <div class="flex flex-wrap items-center gap-2">
        <h3 class="text-sm font-semibold">
          {{
            index?.name ||
            (selected.index_url === DEFAULT_MARKETPLACE_INDEX_URL
              ? t('taskPlugins.official')
              : selected.name)
          }}
        </h3>
        <StatusChip
          v-if="isDefaultMarketplaceSource(selected.index_url)"
          tone="neutral"
          >{{ t('taskPlugins.official') }}</StatusChip
        >
        <StatusChip v-else tone="danger">{{
          t('taskPlugins.thirdPartyRisk')
        }}</StatusChip>
        <StatusChip v-if="missingHashes" tone="danger"
          ><TriangleAlert :size="12" aria-hidden="true" />{{
            t('taskPlugins.noIntegrityVerification')
          }}</StatusChip
        >
      </div>
      <div
        v-if="indexLoading"
        class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3"
      >
        <SkeletonBlock v-for="i in 3" :key="i" rounded="xl" class="h-40" />
      </div>
      <PluginAlert
        v-if="indexError"
        tone="danger"
        role="alert"
        :title="t('taskPlugins.loadSourceFailed')"
      >
        <template #icon><TriangleAlert :size="16" /></template>
        {{ t('taskPlugins.loadSourceFailedDesc', { message: indexError }) }}
      </PluginAlert>
      <p
        v-if="index && !index.plugins.length"
        class="text-sm text-[var(--text-tertiary)]"
      >
        {{ t('taskPlugins.sourceEmpty') }}
      </p>
      <div
        v-if="index && index.plugins.length"
        class="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:gap-4 xl:grid-cols-3"
      >
        <MarketplacePluginCard
          v-for="plugin in index.plugins"
          :key="plugin.key"
          :plugin="plugin"
          :index-url="selected.index_url"
          :install-state="deriveInstallState(plugin, props.installed)"
          :installed="
            props.installed.find((item) => item.meta.key === plugin.key)
          "
          @install="
            installTarget = {
              source: selected,
              plugin,
              version: plugin.latest,
              installState: deriveInstallState(plugin, props.installed),
            }
          "
        />
      </div>
    </section>
    <MarketplaceSourcesDialog
      v-if="sourcesOpen"
      :sources="sources ?? []"
      @close="sourcesOpen = false"
      @saved="sourcesSaved"
    />
    <MarketplaceInstallDialog
      v-if="installTarget"
      :key="`${installTarget.source.index_url}-${installTarget.plugin.key}-${installTarget.version}`"
      :target="installTarget"
      @close="installTarget = null"
      @installed="onInstalled"
    />
  </div>
</template>
