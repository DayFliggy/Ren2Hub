<script setup lang="ts">
// Upstream components/marketplace-capabilities.tsx. Static source declarations
// and marketplace hints retain their distinct provenance.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ErrorBanner from '@/components/common/ErrorBanner.vue'

import { channelTypeLabel } from '../lib/channel-type'
import { resolvePluginMetaPreview } from '../lib/plugin-meta-preview'
import type {
  MarketplaceIndexVersion,
  MarketplacePlugin,
  PluginMetaPreview,
  PluginPreviewField,
} from '../types'
import PluginEndpoints from './PluginEndpoints.vue'
import PluginModelList from './PluginModelList.vue'

const props = withDefaults(
  defineProps<{
    /** Omitted for a manual upload: only the source declarations are shown. */
    plugin?: MarketplacePlugin
    version?: MarketplaceIndexVersion
    preview?: PluginMetaPreview
  }>(),
  { plugin: undefined, version: undefined, preview: undefined }
)
const emit = defineEmits<{ retry: [] }>()
const { t } = useI18n()
const fields = computed(() =>
  resolvePluginMetaPreview(props.plugin, props.version, props.preview)
)
const fromIndex = (field: PluginPreviewField<unknown>) =>
  field.state === 'value' && field.origin === 'index'

function endpointNote(field: PluginPreviewField<unknown[]>, label: string) {
  if (field.state === 'value' && field.value.length > 0)
    return field.origin === 'index'
      ? `${label} · ${t('taskPlugins.fromIndex')}`
      : ''
  const message =
    field.state === 'unknown'
      ? t('taskPlugins.infoUnavailable')
      : t('taskPlugins.notDeclaredByPlugin')
  return `${label}: ${message}`
}

const connections = computed(() => {
  const value = fields.value
  const unavailable = t('taskPlugins.infoUnavailable')
  let channels = unavailable
  if (value.channelTypes.state === 'missing')
    channels = t('taskPlugins.taskPlugin')
  if (value.channelTypes.state === 'value')
    channels = value.channelTypes.value.length
      ? value.channelTypes.value
          .map(
            (type) =>
              `${channelTypeLabel(type, t('taskPlugins.taskPlugin'))} (#${type})`
          )
          .join(', ')
      : t('taskPlugins.taskPlugin')
  let hosts = unavailable
  if (value.allowedHosts.state === 'missing')
    hosts = t('taskPlugins.noExtraDomains')
  if (value.allowedHosts.state === 'value')
    hosts =
      value.allowedHosts.value.join(', ') || t('taskPlugins.noExtraDomains')
  let auth = unavailable
  if (value.auth.state === 'missing') auth = t('taskPlugins.authNotDeclared')
  if (value.auth.state === 'value') {
    auth = value.auth.value || t('taskPlugins.authNotDeclared')
    if (auth === 'api_key') auth = t('taskPlugins.apiKey')
    if (auth === 'none') auth = t('taskPlugins.authNone')
    if (auth === 'oauth2_jwt' || auth === 'vertex_oauth')
      auth = t('taskPlugins.authOauth')
  }
  let baseUrl = unavailable
  if (value.baseUrl.state === 'missing')
    baseUrl = t('taskPlugins.notDeclaredByPlugin')
  if (value.baseUrl.state === 'value')
    baseUrl = value.baseUrl.value || t('taskPlugins.notDeclaredByPlugin')
  return [
    {
      title: t('taskPlugins.applicableChannels'),
      value: channels,
      field: value.channelTypes,
      hint: t('taskPlugins.applicableChannelsHint'),
    },
    {
      title: t('taskPlugins.upstreamAuth'),
      value: auth,
      field: value.auth,
      hint: t('taskPlugins.upstreamAuthHint'),
    },
    {
      title: t('taskPlugins.defaultApiAddress'),
      value: baseUrl,
      field: value.baseUrl,
      hint: t('taskPlugins.defaultApiAddressHint'),
    },
    {
      title: t('taskPlugins.extraDomains'),
      value: hosts,
      field: value.allowedHosts,
      hint: t('taskPlugins.extraDomainsHint'),
    },
  ]
})
const indexChip =
  'inline-flex rounded-md border border-[var(--border-subtle)] px-1.5 py-0.5 text-[11px] text-[var(--text-tertiary)]'
</script>

<template>
  <div class="min-w-0 sibling-space-y-5">
    <ErrorBanner
      v-if="preview && preview.status !== 'parsed'"
      :message="`${t('taskPlugins.previewPartialTitle')}: ${t('taskPlugins.previewPartialDesc')}`"
      @retry="emit('retry')"
    />
    <section
      class="min-w-0 sibling-space-y-3"
      :aria-label="t('taskPlugins.supportedModels')"
    >
      <h3 class="text-sm font-medium">
        {{ t('taskPlugins.supportedModels') }}
      </h3>
      <div class="min-w-0 sibling-space-y-1.5">
        <PluginModelList
          v-if="fields.models.state === 'value' && fields.models.value.length"
          :models="fields.models.value"
        />
        <p v-else class="text-xs text-[var(--text-tertiary)]">
          {{
            fields.models.state === 'unknown'
              ? t('taskPlugins.infoUnavailable')
              : t('taskPlugins.notDeclaredByPlugin')
          }}
        </p>
        <span v-if="fromIndex(fields.models)" :class="indexChip">{{
          t('taskPlugins.fromIndex')
        }}</span>
      </div>
    </section>
    <section
      class="min-w-0 rounded-xl border border-[var(--border-subtle)] p-4"
      :aria-label="t('taskPlugins.supportedInterfaces')"
    >
      <PluginEndpoints
        :title="t('taskPlugins.supportedInterfaces')"
        :protocols="
          fields.protocols.state === 'value'
            ? fields.protocols.value
            : undefined
        "
        :routes="
          fields.routes.state === 'value' ? fields.routes.value : undefined
        "
      >
        <template #protocolsNote>
          <p
            v-if="
              endpointNote(fields.protocols, t('taskPlugins.standardProtocols'))
            "
            class="text-xs text-[var(--text-tertiary)]"
          >
            {{
              endpointNote(fields.protocols, t('taskPlugins.standardProtocols'))
            }}
          </p>
        </template>
        <template #routesNote>
          <p
            v-if="endpointNote(fields.routes, t('taskPlugins.pluginRoutes'))"
            class="text-xs text-[var(--text-tertiary)]"
          >
            {{ endpointNote(fields.routes, t('taskPlugins.pluginRoutes')) }}
          </p>
        </template>
      </PluginEndpoints>
    </section>
    <section
      class="sibling-space-y-3"
      :aria-label="t('taskPlugins.connectionSettings')"
    >
      <h3 class="text-sm font-medium">
        {{ t('taskPlugins.connectionSettings') }}
      </h3>
      <dl
        class="grid gap-x-6 gap-y-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-4 sm:grid-cols-2"
      >
        <div
          v-for="(connection, index) in connections"
          :key="connection.title"
          class="min-w-0 sibling-space-y-1.5"
          :class="index >= 2 ? 'sm:col-span-2' : ''"
        >
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ connection.title }}
          </dt>
          <dd class="sibling-space-y-1.5">
            <p class="break-all text-xs leading-relaxed">
              {{ connection.value }}
            </p>
            <span v-if="fromIndex(connection.field)" :class="indexChip">{{
              t('taskPlugins.fromIndex')
            }}</span>
            <p class="text-xs leading-relaxed text-[var(--text-tertiary)]">
              {{ connection.hint }}
            </p>
          </dd>
        </div>
      </dl>
    </section>
    <p class="text-xs leading-relaxed text-[var(--text-tertiary)]">
      {{ t('taskPlugins.previewFootnote') }}
    </p>
  </div>
</template>
