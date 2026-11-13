<script setup lang="ts">
// Upstream components/marketplace-plugin-card.tsx. A sidecar logo is fetched
// only from the index origin, digest-checked, and shown as a data URI.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowUpCircle,
  CheckCircle2,
  Download,
  TriangleAlert,
} from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { localizedText } from '@/utils/localizedText'

import { channelTypeLabel } from '../lib/channel-type'
import {
  findMarketplaceVersion,
  marketplaceBuiltInVersion,
  resolvePluginSourceUrl,
  type InstallState,
} from '../lib/marketplace'
import { fetchPluginIconDataUri } from '../lib/plugin-icon-file'
import type { MarketplacePlugin, TaskPluginListItem } from '../types'
import PluginIcon from './PluginIcon.vue'
import PluginWebsiteLink from './PluginWebsiteLink.vue'

const props = withDefaults(
  defineProps<{
    plugin: MarketplacePlugin
    indexUrl: string
    installState: InstallState
    installed?: TaskPluginListItem
  }>(),
  { installed: undefined }
)
const emit = defineEmits<{ install: [] }>()
const { t, locale } = useI18n()
const description = computed(() =>
  localizedText(props.plugin.description, locale.value)
)
const channelTypes = computed(() => props.plugin.channelTypes ?? [])
const latestEntry = computed(() =>
  findMarketplaceVersion(props.plugin, props.plugin.latest)
)
const builtInVersion = computed(() =>
  marketplaceBuiltInVersion(props.installed)
)
const iconSrc = ref('')
let controller: AbortController | undefined

watch(
  () => [
    props.indexUrl,
    props.plugin.iconFile?.path,
    props.plugin.iconFile?.sha256,
  ],
  async () => {
    controller?.abort()
    iconSrc.value = ''
    const path = props.plugin.iconFile?.path
    const url = path ? resolvePluginSourceUrl(props.indexUrl, path) : null
    if (!url) return
    const request = new AbortController()
    controller = request
    try {
      const icon = await fetchPluginIconDataUri(url, {
        sha256: props.plugin.iconFile?.sha256,
        signal: request.signal,
      })
      if (!request.signal.aborted) iconSrc.value = icon ?? ''
    } catch {
      // Only an abort reaches here; the next watch run owns the icon.
    }
  },
  { immediate: true }
)
onBeforeUnmount(() => controller?.abort())
const label = 'select-none text-[11px] font-medium text-[var(--text-tertiary)]'
</script>

<template>
  <article
    class="flex h-full min-w-0 flex-col gap-2.5 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
    data-marketplace-plugin-card
  >
    <div class="flex flex-wrap items-center gap-2 text-xs">
      <StatusChip v-if="plugin.recommended" tone="accent">{{
        t('pluginExtensions.recommended')
      }}</StatusChip
      ><span v-if="plugin.installs30d !== undefined">{{
        t('pluginExtensions.installs', { count: plugin.installs30d })
      }}</span
      ><span v-if="plugin.code" class="font-mono">{{ plugin.code }}</span
      ><a
        v-if="plugin.author?.url"
        :href="plugin.author.url"
        target="_blank"
        rel="noopener noreferrer"
        >{{ plugin.author.login }}</a
      ><span v-else-if="plugin.author">{{ plugin.author.login }}</span>
    </div>
    <a
      v-if="plugin.repo"
      :href="plugin.repo"
      target="_blank"
      rel="noopener noreferrer"
      class="truncate text-xs text-[var(--accent)]"
      >{{ plugin.repo }}</a
    >
    <div class="flex items-start justify-between gap-2">
      <div class="flex min-w-0 flex-1 items-center gap-2.5">
        <PluginIcon
          class="mt-0.5"
          :plugin="{ ...plugin, iconSrc: iconSrc || undefined }"
          :size="20"
        />
        <div class="min-w-0">
          <h3 class="truncate text-sm font-medium">
            {{ localizedText(plugin.name_i18n, locale) || plugin.name }}
          </h3>
          <div class="truncate font-mono text-xs text-[var(--text-tertiary)]">
            {{ plugin.key }}
          </div>
        </div>
      </div>
      <StatusChip
        v-if="installState.status === 'not_installed'"
        tone="neutral"
        >{{ t('taskPlugins.notInstalled') }}</StatusChip
      >
      <StatusChip
        v-else-if="installState.status === 'up_to_date'"
        tone="success"
        ><CheckCircle2 :size="12" aria-hidden="true" />{{
          t('taskPlugins.upToDate')
        }}</StatusChip
      >
      <StatusChip
        v-else-if="installState.status === 'upgradable'"
        tone="accent"
        :aria-label="
          t('taskPlugins.upgradeAvailable', {
            installed: installState.installedVersion,
            latest: installState.latestVersion,
          })
        "
        ><ArrowUpCircle :size="12" aria-hidden="true" /><span class="font-mono"
          >v{{ installState.installedVersion }} → v{{
            installState.latestVersion
          }}</span
        ></StatusChip
      >
      <StatusChip v-else tone="danger">{{
        t('taskPlugins.installedNotListed', {
          installed: installState.installedVersion,
        })
      }}</StatusChip>
    </div>
    <PluginWebsiteLink :website="plugin.website" />
    <p
      v-if="description"
      class="line-clamp-2 text-xs text-[var(--text-secondary)]"
    >
      {{ description }}
    </p>
    <div class="grid grid-cols-3 gap-x-3 gap-y-1">
      <div class="min-w-0">
        <div :class="label">{{ t('taskPlugins.latestVersion') }}</div>
        <div class="truncate font-mono text-xs">{{ plugin.latest }}</div>
      </div>
      <div class="min-w-0">
        <div :class="label">{{ t('taskPlugins.channelType') }}</div>
        <div class="truncate text-xs">
          {{
            channelTypes.length
              ? channelTypeLabel(channelTypes[0]!, t('taskPlugins.taskPlugin'))
              : t('taskPlugins.taskPlugin')
          }}
        </div>
      </div>
      <div class="min-w-0">
        <div :class="label">{{ t('taskPlugins.models') }}</div>
        <div class="truncate text-xs">{{ plugin.models?.length ?? 0 }}</div>
      </div>
    </div>
    <div v-if="builtInVersion" class="text-xs">
      <span :class="label">{{ t('taskPlugins.versions') }}</span>
      {{ ' ' }}
      <span class="font-mono">{{
        t('taskPlugins.builtInVsMarket', {
          factory: builtInVersion,
          market: plugin.latest,
        })
      }}</span>
    </div>
    <div
      v-if="!latestEntry?.sha256"
      class="flex items-center gap-1 text-xs text-[var(--status-danger-text)]"
    >
      <TriangleAlert :size="12" class="shrink-0" aria-hidden="true" />
      {{ t('taskPlugins.noIntegrityHash') }}
    </div>
    <div class="mt-auto border-t border-[var(--border-subtle)] pt-2">
      <ConsoleButton
        size="sm"
        block
        :variant="
          installState.status === 'up_to_date' ? 'secondary' : 'primary'
        "
        @click="emit('install')"
      >
        <Download :size="14" aria-hidden="true" />
        {{ t('taskPlugins.install') }}
      </ConsoleButton>
    </div>
  </article>
</template>
