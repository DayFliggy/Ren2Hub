<script setup lang="ts">
// Upstream components/marketplace-panel.tsx. Only the selected source is
// requested; alternates stay idle until the administrator switches.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ListRestart,
  RefreshCw,
  Settings2,
  TriangleAlert,
} from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { formatNumber } from '@/utils/format'
import { localizedText } from '@/utils/localizedText'

import { pluginSite } from '../lib/plugin-site'
import { listMarketplaceSources } from '../api'
import {
  deriveInstallState,
  fetchMarketplaceIndex,
  findPluginByCode,
  findRevokedEntry,
  getMarketplaceUpdates,
  indexHasIntegrityHashes,
  isDefaultMarketplaceSource,
  marketplaceSourceDisplayName,
  marketplaceSourceKind,
} from '../lib/marketplace'
import { formatMarketplaceIndexError } from '../lib/marketplace-install'
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
import MarketplaceUpdatesDialog, {
  type MarketplaceUpdateSelection,
} from './MarketplaceUpdatesDialog.vue'
import PluginAlert from './PluginAlert.vue'

const props = withDefaults(
  defineProps<{
    installed: TaskPluginListItem[]
    installedLoading?: boolean
    installedError?: string
  }>(),
  { installedLoading: false, installedError: '' }
)
const emit = defineEmits<{ changed: [] }>()
const { t, locale } = useI18n()

const sources = ref<MarketplaceSource[] | null>(null)
const sourcesError = ref('')
const selectedUrl = ref('')
const index = ref<MarketplaceIndex | null>(null)
const indexLoading = ref(false)
const indexError = ref('')
const sourcesOpen = ref(false)
const sourcesLoading = ref(false)
const installCode = ref('')
const codeLoading = ref(false)
const codeRequest = new AbortController()
async function resolveCode() {
  if (codeLoading.value) return
  codeLoading.value = true
  indexError.value = ''
  try {
    const found = await findPluginByCode(
      installCode.value,
      sources.value ?? [],
      codeRequest.signal
    )
    if (codeRequest.signal.aborted) return
    if (!found) throw new Error(t('common.noResults'))
    installTarget.value = {
      ...found,
      version: found.plugin.latest,
      installState: deriveInstallState(found.plugin, props.installed),
    }
  } catch (cause) {
    if (!codeRequest.signal.aborted)
      indexError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    codeLoading.value = false
  }
}
onBeforeUnmount(() => codeRequest.abort())
const installTarget = ref<MarketplaceInstallTarget | null>(null)
const updateSelection = ref<MarketplaceUpdateSelection | null>(null)
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
    label: marketplaceSourceDisplayName(source, t('taskPlugins.official')),
  }))
)
const missingHashes = computed(() =>
  index.value ? !indexHasIntegrityHashes(index.value) : false
)
const revokedAlerts = computed(() => {
  const currentIndex = index.value
  if (!currentIndex?.revoked?.length) return []
  const seen = new Set<string>()
  return props.installed.flatMap((plugin) => {
    const entry = findRevokedEntry(currentIndex, plugin.source_hash)
    if (!entry || seen.has(entry.sha256)) return []
    seen.add(entry.sha256)
    return [entry]
  })
})
const updates = computed(() =>
  index.value ? getMarketplaceUpdates(index.value, props.installed) : []
)
const readyUpdateCount = computed(
  () => updates.value.filter((item) => !item.skip).length
)
const bulkUpdateDisabled = computed(
  () =>
    !selected.value ||
    indexLoading.value ||
    Boolean(indexError.value) ||
    props.installedLoading ||
    Boolean(props.installedError)
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
    indexError.value = formatMarketplaceIndexError(error, t)
  } finally {
    if (!request.signal.aborted) indexLoading.value = false
  }
}

watch(() => [selected.value?.index_url, pluginSite.value], loadIndex)
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

function openBulkUpdates() {
  if (!selected.value || bulkUpdateDisabled.value || updates.value.length === 0)
    return
  updateSelection.value = { source: selected.value, updates: updates.value }
}
</script>

<template>
  <div class="sibling-space-y-4">
    <div class="flex flex-wrap items-center gap-2">
      <label for="plugin-site">{{ t('pluginExtensions.site') }}</label
      ><select
        id="plugin-site"
        v-model="pluginSite"
        class="rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 text-sm"
      >
        <option value="ai">www.newapi.ai</option>
        <option value="pro">www.newapi.pro</option>
      </select>
    </div>
    <PluginAlert
      v-for="entry in revokedAlerts"
      :key="entry.sha256"
      tone="danger"
      >{{ t('pluginExtensions.revoked', { name: entry.key || entry.sha256 }) }}
      {{ localizedText(entry.reason, locale) }}</PluginAlert
    >
    <div class="flex flex-wrap items-center justify-between gap-2">
      <p class="text-sm text-[var(--text-secondary)]">
        {{ t('taskPlugins.marketHint') }}
      </p>
      <div class="flex flex-wrap shrink-0 gap-2">
        <ConsoleButton
          v-if="updates.length > 0"
          :variant="readyUpdateCount > 0 ? 'primary' : 'secondary'"
          size="sm"
          :disabled="bulkUpdateDisabled"
          @click="openBulkUpdates"
        >
          <ListRestart :size="14" aria-hidden="true" />
          {{
            readyUpdateCount > 0
              ? t('taskPlugins.updateAllCount', {
                  total: formatNumber(readyUpdateCount),
                })
              : t('taskPlugins.skippedCount', {
                  total: formatNumber(updates.length),
                })
          }}
        </ConsoleButton>
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
    <form
      class="flex flex-wrap items-center gap-2"
      @submit.prevent="resolveCode"
    >
      <label for="plugin-install-code" class="text-sm">{{
        t('pluginExtensions.installCode')
      }}</label>
      <input
        id="plugin-install-code"
        v-model="installCode"
        inputmode="numeric"
        pattern="[0-9]{8}"
        maxlength="8"
        :placeholder="t('pluginExtensions.installCodeHint')"
        class="min-w-0 flex-1 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2 text-sm"
      />
      <ConsoleButton
        type="submit"
        size="sm"
        :disabled="
          codeLoading || !/^\d{8}$/.test(installCode) || !sources?.length
        "
        >{{ t('pluginExtensions.importCode') }}</ConsoleButton
      >
    </form>
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
            marketplaceSourceDisplayName(selected, t('taskPlugins.official'))
          }}
        </h3>
        <StatusChip
          v-if="isDefaultMarketplaceSource(selected.index_url)"
          tone="neutral"
          >{{ t('taskPlugins.official') }}</StatusChip
        >
        <StatusChip
          v-else-if="marketplaceSourceKind(selected.index_url) === 'community'"
          tone="neutral"
          >{{ t('pluginExtensions.community') }}</StatusChip
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
    <MarketplaceUpdatesDialog
      v-if="updateSelection"
      :source="updateSelection.source"
      :updates="updateSelection.updates"
      @close="updateSelection = null"
      @changed="emit('changed')"
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
