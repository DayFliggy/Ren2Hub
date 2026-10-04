<script setup lang="ts">
// Upstream components/marketplace-install-dialog.tsx. The browser fetches the
// source, hashes it and blocks on a sha256 mismatch; the server re-hashes on
// upload. Ren2Hub previews metadata on the server and asks for confirmation
// before a diverged or downgrade install.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, Download } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { useToast } from '@/composables/useToast'

import {
  activateTaskPlugin,
  getTaskPlugin,
  installMarketplacePlugin,
  previewTaskPlugin,
} from '../api'
import {
  findMarketplaceVersion,
  replaceConfirmationReason,
  resolvePluginSourceUrl,
  type InstallState,
} from '../lib/marketplace'
import { fetchPluginIconDataUri } from '../lib/plugin-icon-file'
import {
  previewFromServer,
  unavailablePreview,
} from '../lib/plugin-meta-preview'
import {
  computeSourceSha256,
  fetchPluginSourceText,
  PluginSourceFetchError,
} from '../lib/plugin-url'
import type {
  MarketplacePlugin,
  MarketplaceSource,
  PluginMetaPreview,
} from '../types'
import JavaScriptViewer from './JavaScriptViewer.vue'
import MarketplaceCapabilities from './MarketplaceCapabilities.vue'
import PluginAlert from './PluginAlert.vue'
import PluginChangelogPanel from './PluginChangelogPanel.vue'
import PluginConfirmDialog from './PluginConfirmDialog.vue'
import PluginIcon from './PluginIcon.vue'
import PluginIntegrityCheck from './PluginIntegrityCheck.vue'
import SourceDiff from './SourceDiff.vue'

export type MarketplaceInstallTarget = {
  source: MarketplaceSource
  plugin: MarketplacePlugin
  version: string
  installState: InstallState
}

const props = defineProps<{ target: MarketplaceInstallTarget }>()
const emit = defineEmits<{ close: []; installed: [] }>()
const { t } = useI18n()
const toast = useToast()
const plugin = props.target.plugin
const state = props.target.installState
const hasInstalled = state.status !== 'not_installed'
const selectedVersion = ref(props.target.version)
const reviewTab = ref('details')
const entry = computed(() =>
  findMarketplaceVersion(plugin, selectedVersion.value)
)

const loading = ref(false)
const fetchError = ref('')
const fetched = ref<{
  text: string
  digest: string | null
  icon: string | null
} | null>(null)
const preview = ref<PluginMetaPreview | undefined>()
const previewMeta = ref<Record<string, unknown> | null>(null)
const installedSource = ref<string | null>(null)
const installedError = ref('')
const installing = ref(false)
const installError = ref('')
const confirmOpen = ref(false)
let controller: AbortController | undefined

const message = (error: unknown) =>
  error instanceof Error ? error.message : String(error)

async function loadSource() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  fetched.value = null
  preview.value = undefined
  previewMeta.value = null
  fetchError.value = ''
  const version = entry.value
  if (!version) return
  const url = resolvePluginSourceUrl(
    props.target.source.index_url,
    version.path
  )
  if (!url) {
    fetchError.value = t('taskPlugins.pathOutside')
    return
  }
  loading.value = true
  try {
    const text = await fetchPluginSourceText(url, request.signal)
    // Computed for display and blocking only; the server re-hashes on upload.
    const digest = await computeSourceSha256(text)
    const iconUrl = plugin.iconFile
      ? resolvePluginSourceUrl(
          props.target.source.index_url,
          plugin.iconFile.path
        )
      : null
    const icon = iconUrl
      ? await fetchPluginIconDataUri(iconUrl, {
          sha256: plugin.iconFile?.sha256,
          signal: request.signal,
        })
      : null
    if (request.signal.aborted) return
    fetched.value = { text, digest, icon }
    try {
      const serverPreview = await previewTaskPlugin(text, request.signal)
      previewMeta.value = serverPreview.meta
      preview.value = previewFromServer(serverPreview)
    } catch {
      // A source the preview cannot parse shows as unreadable; the gateway
      // still validates it on install.
      if (!request.signal.aborted) preview.value = unavailablePreview()
    }
  } catch (error) {
    if (request.signal.aborted) return
    fetchError.value =
      error instanceof PluginSourceFetchError
        ? error.reason === 'too_large'
          ? t('taskPlugins.sourceTooLarge')
          : t('taskPlugins.fetchSourceFailed')
        : message(error)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

async function loadInstalled() {
  if (!hasInstalled) return
  try {
    installedSource.value = (await getTaskPlugin(plugin.key)).source
  } catch (error) {
    installedError.value = message(error)
  }
}

watch(selectedVersion, (version) => {
  installError.value = ''
  if (hasInstalled && reviewTab.value !== 'changelog')
    reviewTab.value = version === state.installedVersion ? 'details' : 'diff'
  void loadSource()
})
void loadSource()
void loadInstalled()
onBeforeUnmount(() => controller?.abort())

const digestMismatch = computed(() =>
  Boolean(
    entry.value?.sha256 &&
    fetched.value?.digest &&
    fetched.value.digest.toLowerCase() !== entry.value.sha256.toLowerCase()
  )
)
const validationError = computed(() => {
  const version = entry.value
  if (!version) return ''
  if ((version.minApiVersion ?? 1) > 1) return t('taskPlugins.unsupported')
  const meta = previewMeta.value
  if (!meta) return t('taskPlugins.identityMismatch')
  if (meta.key !== plugin.key || meta.version !== selectedVersion.value)
    return t('taskPlugins.identityMismatch')
  return ''
})
const canInstall = computed(
  () =>
    Boolean(entry.value && fetched.value) &&
    !loading.value &&
    !fetchError.value &&
    !digestMismatch.value &&
    !validationError.value
)
const replaceReason = computed(() =>
  replaceConfirmationReason(plugin, state, selectedVersion.value)
)
const versionOptions = computed(() =>
  plugin.versions.map((version) => {
    const labels = [version.version]
    if (version.version === plugin.latest)
      labels.push(t('taskPlugins.latestVersion'))
    if (
      state.status !== 'not_installed' &&
      version.version === state.installedVersion
    )
      labels.push(t('taskPlugins.activeVersion'))
    return { value: version.version, label: labels.join(' · ') }
  })
)
const tabs = computed(() => [
  { key: 'details', label: t('taskPlugins.pluginDetails') },
  ...(hasInstalled
    ? [{ key: 'diff', label: t('taskPlugins.versionDifferences') }]
    : []),
  { key: 'source', label: t('taskPlugins.fullSource') },
  { key: 'changelog', label: t('taskPlugins.changelog') },
])

function requestInstall() {
  if (!canInstall.value || installing.value) return
  if (replaceReason.value) {
    confirmOpen.value = true
    return
  }
  void install()
}

async function install() {
  const source = fetched.value
  const version = entry.value
  confirmOpen.value = false
  // The digest gate is re-checked here so no caller can bypass it.
  if (!source || !version || digestMismatch.value || validationError.value)
    return
  installing.value = true
  installError.value = ''
  try {
    const detail = await installMarketplacePlugin({
      source: source.text,
      sourceSha256: version.sha256,
      remark: `${props.target.source.name} v${selectedVersion.value}`,
      icon: source.icon ?? undefined,
    })
    // Refresh the installed list immediately after the upload. Activation is
    // a separate operation and may fail even though the version was saved.
    emit('installed')
    if (detail.plugin && !detail.plugin.active) {
      try {
        await activateTaskPlugin(detail.meta.key, detail.meta.version)
      } catch (error) {
        installError.value = message(error)
        return
      }
    }
    toast.success(
      t('taskPlugins.installedToast', {
        name: detail.meta.name,
        version: detail.meta.version,
      })
    )
  } catch (error) {
    // Verbatim: preflight rejections name the conflicting plugin.
    installError.value = message(error)
  } finally {
    installing.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="t('taskPlugins.installName', { name: plugin.name })"
    :subtitle="
      t('taskPlugins.fromSource', {
        key: plugin.key,
        source: target.source.name,
      })
    "
    size="lg"
    :close-disabled="installing"
    @close="emit('close')"
  >
    <div class="space-y-3">
      <div class="flex min-w-0 items-center gap-3">
        <PluginIcon
          :plugin="{ ...plugin, iconSrc: fetched?.icon ?? undefined }"
          :size="36"
        />
        <div class="min-w-0 flex-1 space-y-1">
          <FilterSelect
            v-model="selectedVersion"
            :options="versionOptions"
            :label="t('taskPlugins.selectVersion')"
            :disabled="plugin.versions.length <= 1 || installing"
          />
          <p
            v-if="state.status !== 'not_installed'"
            class="text-xs text-[var(--text-tertiary)]"
          >
            {{
              t('taskPlugins.currentToTarget', {
                from: state.installedVersion,
                to: selectedVersion,
              })
            }}
          </p>
        </div>
      </div>
      <div class="overflow-x-auto">
        <ConsoleTabs
          v-model="reviewTab"
          :items="tabs"
          panel-id="marketplace-install-panel"
          density="compact"
          class="min-w-max"
          :aria-label="t('taskPlugins.pluginDetails')"
        />
      </div>
      <div
        id="marketplace-install-panel"
        role="tabpanel"
        :aria-labelledby="`marketplace-install-panel-tab-${reviewTab}`"
        class="min-w-0 space-y-2"
      >
        <template v-if="reviewTab !== 'changelog'">
          <div
            v-if="loading"
            role="status"
            class="flex items-center gap-2 text-sm text-[var(--text-secondary)]"
          >
            <SkeletonBlock rounded="md" class="h-4 w-4" />
            {{ t('taskPlugins.fetchingSource') }}
          </div>
          <p
            v-if="fetchError"
            role="alert"
            class="text-sm text-[var(--status-danger-text)]"
          >
            {{ fetchError }}
          </p>
          <p
            v-if="validationError"
            role="alert"
            class="text-sm text-[var(--status-danger-text)]"
          >
            {{ validationError }}
          </p>
        </template>
        <MarketplaceCapabilities
          v-if="reviewTab === 'details'"
          :plugin="plugin"
          :version="entry"
          :preview="preview"
          @retry="loadSource"
        />
        <template v-else-if="reviewTab === 'diff'">
          <p
            v-if="installedError"
            role="alert"
            class="text-sm text-[var(--status-danger-text)]"
          >
            {{ installedError }}
          </p>
          <p
            v-else-if="installedSource === null"
            role="status"
            class="text-sm text-[var(--text-secondary)]"
          >
            {{ t('taskPlugins.loadingInstalled') }}
          </p>
          <SourceDiff
            v-else-if="fetched"
            :before="installedSource"
            :after="fetched.text"
          />
        </template>
        <JavaScriptViewer
          v-else-if="reviewTab === 'source' && fetched"
          :value="fetched.text"
          :label="t('taskPlugins.fullSource')"
          class="h-[min(22rem,35vh)]"
        />
        <PluginChangelogPanel
          v-else-if="reviewTab === 'changelog'"
          :index-url="target.source.index_url"
          :plugin="plugin"
          :version="selectedVersion"
        />
      </div>
      <PluginAlert tone="warning">
        <template #icon><AlertTriangle :size="16" /></template>
        {{ t('taskPlugins.reviewWarning') }}
      </PluginAlert>
      <PluginIntegrityCheck
        :source-available="Boolean(fetched)"
        :expected="entry?.sha256"
        :digest="fetched?.digest"
        :loading="loading"
      />
      <PluginAlert
        v-if="state.status === 'diverged'"
        :title="t('taskPlugins.divergedTitle')"
      >
        {{
          t('taskPlugins.divergedDesc', {
            installed: state.installedVersion,
            target: selectedVersion,
          })
        }}
      </PluginAlert>
      <div v-if="installError" role="alert" class="space-y-1">
        <p class="text-sm font-medium text-[var(--status-danger-text)]">
          {{ t('taskPlugins.installFailed') }}
        </p>
        <p class="whitespace-pre-wrap text-sm text-[var(--status-danger-text)]">
          {{ installError }}
        </p>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('taskPlugins.neverForce') }}
        </p>
      </div>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton
          variant="secondary"
          :disabled="installing"
          @click="emit('close')"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          :disabled="!canInstall"
          :loading="installing"
          @click="requestInstall"
        >
          <Download v-if="!installing" :size="15" aria-hidden="true" />
          {{
            installing
              ? t('taskPlugins.installing')
              : t('taskPlugins.installAndEnable')
          }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
  <PluginConfirmDialog
    :open="confirmOpen"
    :title="t('taskPlugins.confirmReplaceTitle')"
    :message="
      state.status === 'not_installed'
        ? ''
        : t(
            replaceReason === 'downgrade'
              ? 'taskPlugins.downgradeDesc'
              : 'taskPlugins.divergedDesc',
            { installed: state.installedVersion, target: selectedVersion }
          )
    "
    destructive
    :confirm-text="t('taskPlugins.confirmReplace')"
    @cancel="confirmOpen = false"
    @confirm="install"
  />
</template>
