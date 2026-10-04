<script setup lang="ts">
// Upstream components/plugin-endpoints.tsx: host protocol endpoints derived
// from each meta.protocols claim, then the plugin's own native routes. Mode
// names and paths are wire vocabulary and stay raw.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import { HOST_PROTOCOL_ENDPOINTS } from '../lib/host-protocols'
import type { TaskPluginProtocolClaim, TaskPluginRoute } from '../types'
import CopyButton from './CopyButton.vue'
import PluginModelList from './PluginModelList.vue'

const props = withDefaults(
  defineProps<{
    title?: string
    protocols?: TaskPluginProtocolClaim[] | null
    routes?: TaskPluginRoute[] | null
  }>(),
  { title: '', protocols: undefined, routes: undefined }
)
const { t } = useI18n()
const claims = computed(() =>
  (props.protocols ?? []).map((claim) => {
    const name = typeof claim === 'string' ? claim : claim.name
    const endpoints = HOST_PROTOCOL_ENDPOINTS[name] ?? []
    return {
      name,
      endpoints,
      supports: typeof claim === 'string' ? [] : (claim.supports ?? []),
      models: typeof claim === 'string' ? [] : (claim.models ?? []),
      // A claim outside the frozen table has no rows; keep its forms visible.
      hasCreateRow: endpoints.some((endpoint) => endpoint.modeBearing),
    }
  })
)
const routes = computed(() => props.routes ?? [])
const modeLabel = (mode: string) =>
  ({
    stream: t('taskPlugins.streaming'),
    sync: t('taskPlugins.synchronous'),
    background: t('taskPlugins.background'),
  })[mode] ?? ''
const routeLabel = (type: TaskPluginRoute['type']) =>
  ({
    submit: t('taskPlugins.submitRequest'),
    query: t('taskPlugins.retrieveResult'),
    dynamic: t('taskPlugins.dynamicOperation'),
  })[type]
const method =
  'mt-1 inline-flex justify-center rounded-md border border-[var(--border-subtle)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--text-secondary)]'
const chip =
  'inline-flex items-center gap-1 rounded-md bg-[var(--surface-solid)] px-1.5 py-0.5 text-[11px] text-[var(--text-secondary)]'
</script>

<template>
  <div class="space-y-3">
    <h3 class="text-sm font-semibold text-[var(--text-primary)]">
      {{ title || t('taskPlugins.endpoints') }}
    </h3>
    <p
      v-if="
        !claims.length &&
        !routes.length &&
        !$slots.protocolsNote &&
        !$slots.routesNote
      "
      class="text-sm text-[var(--text-tertiary)]"
    >
      {{ t('taskPlugins.notDeclared') }}
    </p>
    <slot name="protocolsNote" />
    <p
      v-if="claims.length"
      class="text-xs font-medium text-[var(--text-secondary)]"
    >
      {{ t('taskPlugins.standardProtocols') }}
    </p>
    <div
      v-for="claim in claims"
      :key="claim.name"
      class="space-y-1.5 rounded-lg bg-[var(--surface-muted)] p-3"
    >
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span
          class="min-w-0 break-all font-mono text-xs text-[var(--text-tertiary)]"
          >{{ claim.name }}</span
        >
        <PluginModelList
          v-if="claim.models.length"
          :models="claim.models"
          :collapsed-label="t('taskPlugins.modelScope')"
        />
        <template v-if="!claim.hasCreateRow">
          <span v-for="mode in claim.supports" :key="mode" :class="chip"
            ><span class="font-mono">{{ mode }}</span
            >{{ modeLabel(mode) }}</span
          >
        </template>
      </div>
      <ul class="divide-y divide-[var(--border-subtle)]">
        <li
          v-for="endpoint in claim.endpoints"
          :key="`${endpoint.method} ${endpoint.path}`"
          class="grid grid-cols-[3.5rem_minmax(0,1fr)_auto] items-start gap-2 py-2"
        >
          <span :class="method">{{ endpoint.method }}</span>
          <div class="min-w-0 space-y-1.5 pt-1">
            <span
              class="block max-w-full overflow-x-auto whitespace-nowrap font-mono text-xs"
              :title="endpoint.path"
              >{{ endpoint.path }}</span
            >
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-xs text-[var(--text-tertiary)]">{{
                endpoint.method === 'POST'
                  ? t('taskPlugins.submitRequest')
                  : t('taskPlugins.retrieveResult')
              }}</span>
              <template v-if="endpoint.modeBearing">
                <span v-for="mode in claim.supports" :key="mode" :class="chip"
                  ><span class="font-mono">{{ mode }}</span
                  >{{ modeLabel(mode) }}</span
                >
              </template>
            </div>
          </div>
          <CopyButton
            :value="endpoint.path"
            :label="t('taskPlugins.copyEndpointPath')"
          />
        </li>
      </ul>
    </div>
    <slot name="routesNote" />
    <div
      v-if="routes.length"
      class="space-y-1.5 rounded-lg bg-[var(--surface-muted)] p-3"
    >
      <p class="text-xs text-[var(--text-tertiary)]">
        {{ t('taskPlugins.pluginRoutes') }}
      </p>
      <ul class="divide-y divide-[var(--border-subtle)]">
        <li
          v-for="route in routes"
          :key="`${route.method} ${route.path}`"
          class="grid grid-cols-[3.5rem_minmax(0,1fr)_auto] items-start gap-2 py-2"
        >
          <span :class="method">{{ route.method }}</span>
          <div class="min-w-0 space-y-1.5 pt-1">
            <span
              class="block max-w-full overflow-x-auto whitespace-nowrap font-mono text-xs"
              :title="route.path"
              >{{ route.path }}</span
            >
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="font-mono text-[11px] text-[var(--text-tertiary)]">{{
                route.type
              }}</span>
              <span class="text-xs text-[var(--text-tertiary)]">{{
                routeLabel(route.type)
              }}</span>
              <PluginModelList
                v-if="route.models?.length"
                :models="route.models"
                :collapsed-label="t('taskPlugins.modelScope')"
              />
            </div>
          </div>
          <CopyButton
            :value="route.path"
            :label="t('taskPlugins.copyEndpointPath')"
          />
        </li>
      </ul>
    </div>
  </div>
</template>
