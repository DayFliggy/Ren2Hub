<script setup lang="ts">
import type { ChannelUpstreamRatio } from '@/api/channelUpstreamRatio'
import type { UpstreamBinding } from '@/api/upstreams'
import ChannelBalanceDetails from '@/features/channels/components/ChannelBalanceDetails.vue'
import {
  Activity,
  ChevronDown,
  ChevronRight,
  Coins,
  Gauge,
  GitCompareArrows,
  LoaderCircle,
  Pencil,
  Power,
  PowerOff,
  Scale,
  Trash2,
} from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import CapacityMeter from '@/components/common/CapacityMeter.vue'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import type { ChannelModelTestResult } from '@/composables/useAdminChannels'
import {
  type AdminChannelOptionalField,
  adminChannelResponseText,
  adminChannelResponseTone,
  adminChannelTypeMeta,
  canEditChannel,
  hasInvalidModelMapping,
  isRetiredChannelType,
  labDisplayName,
  supportsBalanceQuery,
} from '@/constants/adminChannels'
import type { AdminChannel } from '@/types/console'
import { formatDecimal } from '@/utils/decimal'
import { formatQuota, relativeTime } from '@/utils/format'

import ChannelInlineNumber from './ChannelInlineNumber.vue'
import ChannelStatusBadge from './ChannelStatusBadge.vue'
import ChannelUpstreamBindingBadge from './ChannelUpstreamBindingBadge.vue'
import UpstreamRatioSummary from './UpstreamRatioSummary.vue'

type ChannelAction = 'priority' | 'weight' | 'status' | 'balance'

/** Batch runs are balance-only; testing opens the detailed test modal. */
interface ChannelBatchProgress {
  scope: 'page' | 'supplier'
  supplier?: string
  total: number
  processed: number
}

const props = withDefaults(
  defineProps<{
    channels?: AdminChannel[]
    groups: Array<{
      groupSlug: string
      supplier: string
      channels: AdminChannel[]
    }>
    grouped?: boolean
    visibleFields: AdminChannelOptionalField[]
    selectedIds: number[]
    allSelected: boolean
    selectionDisabled: boolean
    toggleAllSelected: () => void
    toggleSelected: (channel: AdminChannel) => void
    isSupplierCollapsed: (supplier: string) => boolean
    toggleSupplier: (supplier: string) => void
    batchProgress: ChannelBatchProgress | null
    /** Inline batch-test results, keyed by channel id; overrides persisted
        response_time while a group test is running. */
    groupTestOverrides: Record<number, ChannelModelTestResult | 'running'>
    canRunBatch: boolean
    canMutate: boolean
    canRead?: boolean
    canOperate: boolean
    canWrite: boolean
    canSensitiveWrite: boolean
    runSupplierBalance: (
      supplier: string,
      channels: AdminChannel[],
      groupSlug: string
    ) => Promise<void>
    pickSupplierTest: (group: {
      groupSlug: string
      supplier: string
      channels: AdminChannel[]
    }) => void
    clearSupplier: (group: {
      groupSlug: string
      supplier: string
      channels: AdminChannel[]
    }) => void
    isBusy: (id: number, action: ChannelAction) => boolean
    isRowBusy: (id: number) => boolean
    updateNumber: (
      channel: AdminChannel,
      field: 'priority' | 'weight',
      value: number
    ) => Promise<boolean>
    toggleStatus: (channel: AdminChannel) => Promise<boolean>
    refreshBalance: (channel: AdminChannel) => Promise<boolean>
    openTest: (channel: AdminChannel) => void
    editChannel: (channel: AdminChannel) => void
    syncModels: (channel: AdminChannel) => void
    showRuntime: (channel: AdminChannel) => void
    deleteChannel: (channel: AdminChannel) => void
    /** Upstream bindings keyed by channel id; empty without upstream.read. */
    upstreamBindings?: Record<number, UpstreamBinding>
    /** Upstream ratio records keyed by channel id. */
    upstreamRatios?: Record<number, ChannelUpstreamRatio>
    openUpstreamRatio?: (channel: AdminChannel) => void
  }>(),
  {
    channels: undefined,
    grouped: true,
    canRead: true,
    upstreamBindings: () => ({}),
    upstreamRatios: () => ({}),
    openUpstreamRatio: undefined,
  }
)

const { t, locale } = useI18n()

const flatChannels = computed<AdminChannel[]>(
  () => props.channels ?? props.groups.flatMap((group) => group.channels)
)

const renderedSections = computed(() =>
  props.grouped
    ? props.groups.map((group) => ({
        key: group.supplier,
        group,
        channels: group.channels,
      }))
    : [{ key: 'flat', group: null, channels: flatChannels.value }]
)

/** Response cell content: batch-test override first, persisted value after. */
function responseDisplay(channel: AdminChannel): {
  tone: 'neutral' | 'success' | 'warning' | 'danger'
  text: string
  title: string
  running: boolean
} {
  const override = props.groupTestOverrides[channel.id]
  if (override === 'running') {
    return {
      tone: 'warning',
      text: t('channels.testStatusRunning'),
      title: t('channels.testStatusRunning'),
      running: true,
    }
  }
  if (override) {
    if (override.ok) {
      return {
        tone: 'success',
        text: adminChannelResponseText(
          override.timeMs ?? 0,
          t('channels.notTested')
        ),
        title: t('channels.testStatusSuccess'),
        running: false,
      }
    }
    return {
      tone: 'danger',
      text: t('channels.testStatusFailed'),
      title: override.message ?? t('channels.testStatusFailed'),
      running: false,
    }
  }
  return {
    tone: adminChannelResponseTone(channel.response_time),
    text: adminChannelResponseText(
      channel.response_time,
      t('channels.notTested')
    ),
    title: channel.test_time
      ? t('channels.testedAt', {
          time: relativeTime(channel.test_time, locale.value),
        })
      : t('channels.notTested'),
    running: false,
  }
}

function rowMenuItems(channel: AdminChannel): ActionMenuItem[] {
  const items: ActionMenuItem[] = []
  const retired = isRetiredChannelType(channel.type)
  if (!retired && props.canOperate) {
    items.push({
      key: 'sync-models',
      label: t('channels.syncModelsTitle'),
      icon: GitCompareArrows,
      disabled: !props.canMutate,
    })
  }
  if (!retired && props.canRead) {
    items.push({
      key: 'runtime',
      label: t('gateway.channelRuntime'),
      icon: Gauge,
    })
  }
  if (
    !retired &&
    props.canOperate &&
    (!props.visibleFields.includes('response') ||
      !props.visibleFields.includes('rowResponseAction'))
  ) {
    items.push({
      key: 'test',
      label: t('channels.testChannel'),
      icon: Activity,
    })
  }
  if (
    !retired &&
    props.canRead &&
    props.openUpstreamRatio &&
    props.upstreamRatios?.[channel.id]
  ) {
    items.push({
      key: 'upstream-ratio',
      label: t('channels.upstreamRatio.open'),
      icon: Scale,
    })
  }
  if (props.canSensitiveWrite) {
    items.push({
      key: 'delete',
      label: t('channels.deleteChannel'),
      icon: Trash2,
      tone: 'danger',
      separated: items.length > 0,
      disabled: props.isRowBusy(channel.id),
    })
  }
  return items
}

function onRowMenuSelect(channel: AdminChannel, key: string) {
  if (key === 'sync-models') props.syncModels(channel)
  else if (key === 'runtime') props.showRuntime(channel)
  else if (key === 'test') props.openTest(channel)
  else if (key === 'upstream-ratio') props.openUpstreamRatio?.(channel)
  else if (key === 'delete') props.deleteChannel(channel)
}
</script>

<template>
  <div>
    <div
      v-if="canOperate || canSensitiveWrite"
      class="flex items-center gap-1 border-b border-[var(--border-subtle)] px-4 py-1 text-xs text-[var(--text-secondary)]"
    >
      <label
        class="-ml-3 inline-flex shrink-0 cursor-pointer items-center gap-1"
        :title="t('channels.selectPage')"
      >
        <input
          type="checkbox"
          class="checkbox-round"
          :checked="allSelected"
          :disabled="selectionDisabled"
          :aria-label="t('channels.selectPage')"
          @change="toggleAllSelected"
        />
        <span class="select-none">{{ t('channels.selectPage') }}</span>
      </label>
    </div>
    <div class="sibling-divide-y sibling-divide-[var(--border-default)]">
      <section v-for="section in renderedSections" :key="section.key">
        <div
          v-if="section.group"
          data-channel-mobile-group
          class="flex min-w-0 items-center justify-between gap-2 bg-[var(--surface-muted)] px-3 py-2.5"
        >
          <button
            type="button"
            class="flex min-w-0 flex-1 items-center gap-2 text-left focus-ring"
            :aria-label="
              isSupplierCollapsed(section.group.supplier)
                ? t('channels.expandSupplier', {
                    supplier: section.group.supplier,
                  })
                : t('channels.collapseSupplier', {
                    supplier: section.group.supplier,
                  })
            "
            :aria-expanded="!isSupplierCollapsed(section.group.supplier)"
            @click="toggleSupplier(section.group.supplier)"
          >
            <ChevronRight
              v-if="isSupplierCollapsed(section.group.supplier)"
              :size="15"
              class="shrink-0 text-[var(--text-tertiary)]"
            />
            <ChevronDown
              v-else
              :size="15"
              class="shrink-0 text-[var(--text-tertiary)]"
            />
            <VendorLogo :vendor="section.group.supplier" :size="24" />
            <span class="min-w-0 flex-1">
              <span
                class="block truncate text-sm font-semibold text-[var(--text-primary)]"
              >
                {{ section.group.supplier }}
              </span>
              <span class="block text-[0.625rem] text-[var(--text-tertiary)]">
                {{
                  batchProgress?.scope === 'supplier' &&
                  batchProgress.supplier === section.group.supplier
                    ? t('channels.batchProgress', {
                        processed: batchProgress.processed,
                        total: batchProgress.total,
                      })
                    : t('channels.supplierGroupCount', {
                        count: section.group.channels.length,
                      })
                }}
              </span>
            </span>
          </button>
          <div
            v-if="canOperate || canSensitiveWrite"
            class="flex shrink-0 items-center gap-0.5"
          >
            <IconButton
              v-if="
                canOperate && section.group.channels.some(supportsBalanceQuery)
              "
              :label="
                t('channels.syncSupplier', { supplier: section.group.supplier })
              "
              :disabled="!canRunBatch"
              @click="
                runSupplierBalance(
                  section.group.supplier,
                  section.group.channels,
                  section.group.groupSlug
                )
              "
            >
              <LoaderCircle
                v-if="
                  batchProgress?.scope === 'supplier' &&
                  batchProgress.supplier === section.group.supplier
                "
                :size="14"
                class="animate-spin"
              />
              <Coins v-else :size="14" />
            </IconButton>
            <IconButton
              v-if="
                canOperate &&
                section.group.channels.some(
                  (channel) => !isRetiredChannelType(channel.type)
                )
              "
              :label="
                t('channels.testSupplier', { supplier: section.group.supplier })
              "
              @click="pickSupplierTest(section.group)"
            >
              <Activity :size="14" />
            </IconButton>
            <IconButton
              v-if="
                canSensitiveWrite &&
                section.group.channels.some(
                  (channel) => !isRetiredChannelType(channel.type)
                )
              "
              :label="
                t('channels.clearSupplier', {
                  supplier: section.group.supplier,
                })
              "
              tone="danger"
              :disabled="!canMutate"
              @click="clearSupplier(section.group)"
            >
              <Trash2 :size="14" />
            </IconButton>
          </div>
        </div>

        <div
          v-if="!section.group || !isSupplierCollapsed(section.group.supplier)"
          class="sibling-divide-y sibling-divide-[var(--border-subtle)]"
        >
          <article
            v-for="channel in section.channels"
            :key="channel.id"
            data-channel-mobile-row
            class="min-w-0 px-4 py-4 transition-colors"
            :class="
              channel.status === 1 && !isRetiredChannelType(channel.type)
                ? ''
                : 'bg-[var(--surface-muted)]'
            "
          >
            <header class="flex min-w-0 items-start justify-between gap-3">
              <div class="flex min-w-0 items-start gap-2">
                <label
                  v-if="canOperate || canSensitiveWrite"
                  class="-ml-3 -mt-2 flex shrink-0 items-center justify-center"
                  @click.stop
                >
                  <input
                    type="checkbox"
                    class="checkbox-round"
                    :checked="selectedIds.includes(channel.id)"
                    :disabled="
                      selectionDisabled || isRetiredChannelType(channel.type)
                    "
                    :aria-label="
                      t('channels.selectChannel', { name: channel.name })
                    "
                    @change="toggleSelected(channel)"
                  />
                </label>
                <VendorLogo
                  :vendor="labDisplayName(channel.lab)"
                  :size="28"
                  class="mt-0.5 shrink-0"
                />
                <div class="min-w-0">
                  <h2
                    class="truncate text-sm font-semibold"
                    :class="
                      channel.status === 1 &&
                      !isRetiredChannelType(channel.type)
                        ? 'text-[var(--text-primary)]'
                        : 'text-[var(--text-secondary)]'
                    "
                    :title="channel.name"
                  >
                    {{ channel.name }}
                  </h2>
                  <StatusChip
                    v-if="hasInvalidModelMapping(channel.lab)"
                    class="mt-1"
                    tone="warning"
                    data-channel-invalid-mapping
                  >
                    {{ t('channels.labInvalidMapping') }}
                  </StatusChip>
                  <ChannelUpstreamBindingBadge
                    v-if="upstreamBindings?.[channel.id]"
                    :binding="upstreamBindings[channel.id]!"
                  />
                  <p
                    v-if="
                      visibleFields.includes('id') ||
                      visibleFields.includes('type')
                    "
                    class="mt-0.5 text-xs text-[var(--text-tertiary)]"
                  >
                    <span v-if="visibleFields.includes('id')" class="font-mono">
                      #{{ channel.id }}
                    </span>
                    <span
                      v-if="
                        visibleFields.includes('id') &&
                        visibleFields.includes('type')
                      "
                      aria-hidden="true"
                    >
                      ·
                    </span>
                    <span v-if="visibleFields.includes('type')">
                      {{ adminChannelTypeMeta(channel.type).label }}
                    </span>
                  </p>
                </div>
              </div>
              <div class="flex shrink-0 flex-col items-end gap-1">
                <ChannelStatusBadge
                  v-if="
                    visibleFields.includes('status') ||
                    isRetiredChannelType(channel.type)
                  "
                  :channel="channel"
                  mobile
                />
              </div>
            </header>
            <p
              v-if="isRetiredChannelType(channel.type)"
              class="mt-3 text-xs text-[var(--text-secondary)]"
            >
              {{ t('channels.retiredHint') }}
            </p>

            <dl
              class="mt-4 grid min-w-0 grid-cols-2 gap-x-4 gap-y-4 text-xs sm:grid-cols-3"
            >
              <div v-if="visibleFields.includes('priority')" class="min-w-0">
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('channels.priority') }}
                </dt>
                <dd class="mt-1.5">
                  <ChannelInlineNumber
                    v-if="canWrite && !isRetiredChannelType(channel.type)"
                    :value="channel.priority"
                    :min="Number.MIN_SAFE_INTEGER"
                    :max="Number.MAX_SAFE_INTEGER"
                    :label="t('channels.priorityFor', { name: channel.name })"
                    :busy="isRowBusy(channel.id)"
                    :commit="
                      (value) => updateNumber(channel, 'priority', value)
                    "
                  />
                  <span v-else class="tabular-nums text-[var(--text-primary)]">
                    {{ channel.priority }}
                  </span>
                  <small
                    v-if="channel.routing_quality"
                    class="mt-0.5 block truncate text-[var(--text-tertiary)]"
                    :title="
                      t('channels.qualitySummary', {
                        ...channel.routing_quality,
                        reason: t(
                          'channels.qualityReasons.' +
                            channel.routing_quality.reason
                        ),
                      })
                    "
                    >{{
                      t('channels.qualitySummary', {
                        ...channel.routing_quality,
                        reason: t(
                          'channels.qualityReasons.' +
                            channel.routing_quality.reason
                        ),
                      })
                    }}</small
                  >
                </dd>
              </div>
              <div v-if="visibleFields.includes('weight')" class="min-w-0">
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('channels.weight') }}
                </dt>
                <dd class="mt-1.5">
                  <ChannelInlineNumber
                    v-if="canWrite && !isRetiredChannelType(channel.type)"
                    :value="channel.weight"
                    :label="t('channels.weightFor', { name: channel.name })"
                    :busy="isRowBusy(channel.id)"
                    :commit="(value) => updateNumber(channel, 'weight', value)"
                  />
                  <span v-else class="tabular-nums text-[var(--text-primary)]">
                    {{ channel.weight }}
                  </span>
                </dd>
              </div>
              <div v-if="visibleFields.includes('capacity')" class="min-w-0">
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('channels.capacity') }}
                </dt>
                <dd class="mt-1.5">
                  <CapacityMeter
                    v-if="channel.max_concurrency > 0"
                    :used="channel.in_flight"
                    :total="channel.max_concurrency"
                    min-width="84px"
                  />
                  <span
                    v-else
                    class="font-mono tabular-nums text-[var(--text-primary)]"
                  >
                    {{ channel.in_flight }} / ∞
                  </span>
                </dd>
              </div>
              <div v-if="visibleFields.includes('usage')" class="min-w-0">
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('channels.usageAndRatio') }}
                </dt>
                <dd class="mt-1">
                  <p
                    class="font-semibold tabular-nums text-[var(--text-primary)]"
                  >
                    {{ formatQuota(channel.used_quota) }}
                  </p>
                  <p class="mt-1 text-[0.6875rem] text-[var(--text-tertiary)]">
                    {{
                      t('channels.channelRatioValue', {
                        ratio: formatDecimal(channel.channel_ratio),
                      })
                    }}
                  </p>
                </dd>
              </div>
              <div v-if="visibleFields.includes('upstream')" class="min-w-0">
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('channels.upstreamAndRatio') }}
                </dt>
                <dd class="mt-1 flex items-start justify-between gap-1">
                  <div class="min-w-0 sibling-space-y-1">
                    <ChannelBalanceDetails :channel="channel" />
                    <button
                      v-if="upstreamRatios?.[channel.id]"
                      type="button"
                      data-channel-mobile-upstream-ratio
                      class="flex flex-wrap items-center gap-1 text-left text-[0.6875rem] text-[var(--text-tertiary)] hover:text-[var(--text-primary)] focus-ring"
                      :disabled="!canRead || !openUpstreamRatio"
                      @click="openUpstreamRatio?.(channel)"
                    >
                      <Scale :size="12" aria-hidden="true" />
                      <UpstreamRatioSummary
                        :view="upstreamRatios[channel.id]!"
                      />
                    </button>
                  </div>
                  <IconButton
                    v-if="
                      canOperate &&
                      supportsBalanceQuery(channel) &&
                      visibleFields.includes('rowUpstreamAction')
                    "
                    :label="t('channels.refreshBalance')"
                    :disabled="isRowBusy(channel.id)"
                    @click="refreshBalance(channel)"
                  >
                    <LoaderCircle
                      v-if="isBusy(channel.id, 'balance')"
                      :size="14"
                      class="animate-spin"
                    />
                    <Coins v-else :size="14" />
                  </IconButton>
                </dd>
              </div>
              <div v-if="visibleFields.includes('response')" class="min-w-0">
                <dt class="text-[var(--text-tertiary)]">
                  {{ t('channels.response') }}
                </dt>
                <dd class="mt-1 flex items-center justify-between gap-1">
                  <StatusChip
                    class="overflow-visible whitespace-nowrap"
                    :tone="responseDisplay(channel).tone"
                    :title="responseDisplay(channel).title"
                  >
                    <LoaderCircle
                      v-if="responseDisplay(channel).running"
                      :size="12"
                      class="relative z-[1] shrink-0 animate-spin overflow-visible"
                      data-channel-response-spinner
                    />
                    {{ responseDisplay(channel).text }}
                  </StatusChip>
                  <IconButton
                    v-if="
                      canOperate &&
                      !isRetiredChannelType(channel.type) &&
                      visibleFields.includes('rowResponseAction')
                    "
                    :label="t('channels.testChannel')"
                    @click="openTest(channel)"
                  >
                    <Activity :size="14" />
                  </IconButton>
                </dd>
              </div>
            </dl>

            <footer
              class="mt-4 flex items-center justify-end gap-1 border-t border-[var(--border-subtle)] pt-3"
            >
              <template v-if="isRetiredChannelType(channel.type)">
                <IconButton
                  v-if="canSensitiveWrite"
                  :label="t('channels.deleteChannel')"
                  tone="danger"
                  :disabled="isRowBusy(channel.id)"
                  @click="deleteChannel(channel)"
                >
                  <Trash2 :size="16" />
                </IconButton>
              </template>
              <template v-else>
                <IconButton
                  v-if="canEditChannel(channel, canWrite)"
                  :label="t('channels.editChannel')"
                  :disabled="isRowBusy(channel.id)"
                  @click="editChannel(channel)"
                >
                  <Pencil :size="16" />
                </IconButton>
                <IconButton
                  v-if="canOperate"
                  :label="
                    channel.status === 1
                      ? t('channels.disableChannel')
                      : t('channels.enableChannel')
                  "
                  :tone="channel.status === 1 ? 'danger' : 'default'"
                  :disabled="isRowBusy(channel.id)"
                  @click="toggleStatus(channel)"
                >
                  <LoaderCircle
                    v-if="isBusy(channel.id, 'status')"
                    :size="16"
                    class="animate-spin"
                  />
                  <PowerOff v-else-if="channel.status === 1" :size="16" />
                  <Power v-else :size="16" />
                </IconButton>
                <ActionMenu
                  v-if="rowMenuItems(channel).length > 0"
                  :items="rowMenuItems(channel)"
                  :label="t('channels.rowMoreActions')"
                  @select="onRowMenuSelect(channel, $event)"
                />
              </template>
            </footer>
          </article>
        </div>
      </section>
    </div>
  </div>
</template>
