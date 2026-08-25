<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  GripVertical,
  Plus,
  RefreshCw,
  Save,
  Trash2,
} from 'lucide-vue-next'

import { routingApi } from '@/api/routingApi'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FormField from '@/components/common/FormField.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'
import type {
  EligibleRouteChannel,
  RouteEntry,
  RouteGroup,
  RouteHealthState,
  RouteCatalog,
  RoutePolicy,
  RoutePreview,
  RouteProfileInput,
  RouteProfileView,
} from '@/types/routing'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const toast = useToast()

const tokenId = computed(() => Number(route.params.id))
const profileView = ref<RouteProfileView | null>(null)
const groups = ref<RouteGroup[]>([])
const channels = ref<EligibleRouteChannel[]>([])
const catalog = ref<RouteCatalog | null>(null)
const model = ref('')
const path = ref('/v1/chat/completions')
const selectedChannelId = ref('')
const preview = ref<RoutePreview | null>(null)
const loading = ref(true)
const saving = ref(false)
const loadingPreview = ref(false)
const loadError = ref('')
const conflict = ref(false)
const draggingEntry = ref<RouteEntry | null>(null)
const activeGroupKey = ref('')

const activeGroup = computed(() => {
  return (
    groups.value.find((group) => groupKey(group) === activeGroupKey.value) ??
    groups.value[0] ??
    null
  )
})

const activeEntryIds = computed(
  () =>
    new Set(activeGroup.value?.entries.map((entry) => entry.channel_id) ?? [])
)

const availableChannels = computed(() =>
  channels.value.filter((channel) => !activeEntryIds.value.has(channel.id))
)

function defaultPolicy(groupId = 0): RoutePolicy {
  return {
    group_id: groupId,
    load_balance: false,
    max_ratio: 1,
    retry_mode: 'next_channel',
    max_same_resource_attempts: 0,
    max_failover_attempts: 1,
    sticky: false,
  }
}

function groupKey(group: RouteGroup): string {
  return group.id > 0 ? `id:${group.id}` : `new:${group.position}`
}

function newGroup(position: number): RouteGroup {
  return {
    id: 0,
    profile_id: profileView.value?.profile.id ?? 0,
    name: t('routing.defaultGroupName', { position: position + 1 }),
    kind: 'manual',
    enabled: true,
    position,
    entries: [],
    policy: defaultPolicy(),
  }
}

function setView(view: RouteProfileView): void {
  profileView.value = view
  groups.value = view.groups.map((group) => ({
    ...group,
    entries: group.entries.map((entry) => ({ ...entry })),
    policy: { ...group.policy },
  }))
  activeGroupKey.value = view.profile.active_group_id
    ? `id:${view.profile.active_group_id}`
    : groups.value[0]
      ? groupKey(groups.value[0])
      : ''
  conflict.value = false
}

async function load(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    const [profiles, eligible, capabilityCatalog] = await Promise.all([
      routingApi.profiles(),
      routingApi.eligibleChannels(),
      routingApi.catalog(),
    ])
    channels.value = eligible
    catalog.value = capabilityCatalog
    const existing = profiles.find(
      (item) => item.profile.token_id === tokenId.value
    )
    if (existing) setView(existing)
    else {
      profileView.value = null
      groups.value = []
      activeGroupKey.value = ''
    }
  } catch (error) {
    loadError.value = error instanceof ApiError ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

function groupInput(group: RouteGroup): RouteProfileInput['groups'][number] {
  return {
    ...(group.id > 0 ? { id: group.id } : {}),
    name: group.name.trim(),
    kind: 'manual',
    enabled: group.enabled,
    position: group.position,
    entries: group.entries.map((entry) => ({
      ...(entry.id > 0 ? { id: entry.id } : {}),
      channel_id: entry.channel_id,
      source: 'platform',
      enabled: entry.enabled,
      position: entry.position,
      weight: entry.weight,
    })),
    policy: {
      load_balance: group.policy.load_balance,
      max_ratio: group.policy.max_ratio,
      retry_mode: group.policy.retry_mode,
      max_same_resource_attempts: group.policy.max_same_resource_attempts,
      max_failover_attempts: group.policy.max_failover_attempts,
      sticky: group.policy.sticky,
    },
  }
}

function input(): RouteProfileInput {
  const active = activeGroup.value
  const activeGroupIndex = active ? groups.value.indexOf(active) : -1
  return {
    ...(profileView.value
      ? { version: profileView.value.profile.version }
      : { token_id: tokenId.value }),
    mode: 'manual',
    active_group_id:
      active && active.id > 0
        ? active.id
        : activeGroupIndex >= 0
          ? -(activeGroupIndex + 1)
          : null,
    groups: groups.value.map(groupInput),
  }
}

async function save(): Promise<void> {
  if (saving.value || !tokenId.value) return
  saving.value = true
  try {
    const next = profileView.value
      ? await routingApi.update(profileView.value.profile.id, input())
      : await routingApi.create(input())
    setView(next)
    toast.success(t('routing.saved'))
  } catch (error) {
    if (error instanceof ApiError && error.code === 'VERSION_CONFLICT') {
      conflict.value = true
      toast.warning(t('routing.versionConflict'))
    } else {
      toast.error(error instanceof ApiError ? error.message : String(error))
    }
  } finally {
    saving.value = false
  }
}

function addGroup(): void {
  const group = newGroup(groups.value.length)
  groups.value = [...groups.value, group]
  if (!activeGroupKey.value) activeGroupKey.value = groupKey(group)
}

function removeGroup(group: RouteGroup): void {
  const currentActive = activeGroup.value
  const next = groups.value.filter((item) => item !== group)
  next.forEach((item, index) => (item.position = index))
  groups.value = next
  if (groupKey(group) === activeGroupKey.value) {
    activeGroupKey.value = next[0] ? groupKey(next[0]) : ''
  } else if (currentActive && next.includes(currentActive)) {
    activeGroupKey.value = groupKey(currentActive)
  }
}

function activateGroup(group: RouteGroup): void {
  activeGroupKey.value = groupKey(group)
}

function addChannel(): void {
  const group = activeGroup.value
  const channelId = Number(selectedChannelId.value)
  if (!group || !channelId || activeEntryIds.value.has(channelId)) return
  const entry: RouteEntry = {
    id: 0,
    group_id: group.id,
    channel_id: channelId,
    source: 'platform',
    enabled: true,
    position: group.entries.length,
    weight: 100,
  }
  group.entries.push(entry)
  selectedChannelId.value = ''
}

function removeEntry(entry: RouteEntry): void {
  const group = activeGroup.value
  if (!group) return
  group.entries = group.entries
    .filter((item) => item !== entry)
    .map((item, index) => ({ ...item, position: index }))
}

function moveEntry(entry: RouteEntry, offset: number): void {
  const group = activeGroup.value
  if (!group) return
  const index = group.entries.indexOf(entry)
  const target = index + offset
  if (index < 0 || target < 0 || target >= group.entries.length) return
  const entries = [...group.entries]
  ;[entries[index], entries[target]] = [entries[target], entries[index]]
  group.entries = entries.map((item, position) => ({ ...item, position }))
}

function dropEntry(target: RouteEntry): void {
  if (draggingEntry.value === null || draggingEntry.value === target) return
  const group = activeGroup.value
  if (!group) return
  const from = group.entries.indexOf(draggingEntry.value)
  const to = group.entries.indexOf(target)
  if (from < 0 || to < 0) return
  const entries = [...group.entries]
  const [moved] = entries.splice(from, 1)
  entries.splice(to, 0, moved)
  group.entries = entries.map((item, position) => ({ ...item, position }))
  draggingEntry.value = null
}

function channelFor(entry: RouteEntry): EligibleRouteChannel | undefined {
  return channels.value.find((channel) => channel.id === entry.channel_id)
}

function capabilityFor(entry: RouteEntry) {
  return catalog.value?.items.find(
    (item) => item.channel_id === entry.channel_id
  )
}

function channelTone(
  channel: EligibleRouteChannel | undefined
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (!channel || channel.filter_reason) return 'danger'
  if (channel.capability_state !== 'eligible') return 'warning'
  return 'success'
}

function healthTone(state: RouteHealthState): 'success' | 'warning' | 'danger' {
  if (state === 'closed') return 'success'
  if (state === 'half_open') return 'warning'
  return 'danger'
}

async function runPreview(): Promise<void> {
  if (!profileView.value || !model.value.trim() || !path.value.trim()) return
  loadingPreview.value = true
  try {
    preview.value = await routingApi.preview(profileView.value.profile.id, {
      model: model.value.trim(),
      path: path.value.trim(),
    })
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    loadingPreview.value = false
  }
}

function reloadAfterConflict(): void {
  void load()
  preview.value = null
}

onMounted(() => void load())
</script>

<template>
  <div class="space-y-6" data-handdrawn-page="token-routing">
    <PageBreadcrumb :crumbs="[t('keys.breadcrumb.0'), t('routing.title')]">
      <template #action>
        <div class="flex gap-2">
          <ConsoleButton
            variant="secondary"
            @click="router.push({ name: 'keys' })"
          >
            <ArrowLeft class="h-4 w-4" aria-hidden="true" />
            {{ t('common.back') }}
          </ConsoleButton>
          <ConsoleButton :loading="saving" :disabled="loading" @click="save">
            <Save class="h-4 w-4" aria-hidden="true" />
            {{ t('common.save') }}
          </ConsoleButton>
        </div>
      </template>
    </PageBreadcrumb>

    <header>
      <h1 class="text-2xl font-semibold text-[var(--text-primary)]">
        {{ t('routing.title') }}
      </h1>
      <p class="mt-1 text-sm text-[var(--text-tertiary)]">
        {{ t('routing.subtitle') }}
      </p>
    </header>

    <div
      v-if="conflict"
      class="flex items-center justify-between gap-4 border border-[var(--status-warning)] bg-[var(--status-warning-soft)] p-4 text-sm"
    >
      <span>{{ t('routing.versionConflictHint') }}</span>
      <ConsoleButton variant="secondary" size="sm" @click="reloadAfterConflict">
        <RefreshCw class="h-4 w-4" aria-hidden="true" />
        {{ t('common.retry') }}
      </ConsoleButton>
    </div>

    <div
      v-if="loading"
      class="py-16 text-center text-sm text-[var(--text-tertiary)]"
    >
      {{ t('common.loading') }}
    </div>
    <div v-else-if="loadError" class="py-16 text-center">
      <p class="text-sm text-[var(--status-danger-text)]">{{ loadError }}</p>
      <ConsoleButton class="mt-5" variant="secondary" @click="load">
        <RefreshCw class="h-4 w-4" aria-hidden="true" />
        {{ t('common.retry') }}
      </ConsoleButton>
    </div>
    <template v-else>
      <ConsoleCard :title="t('routing.groupsTitle')">
        <template #action>
          <ConsoleButton variant="secondary" size="sm" @click="addGroup">
            <Plus class="h-4 w-4" aria-hidden="true" />
            {{ t('routing.addGroup') }}
          </ConsoleButton>
        </template>
        <div v-if="!groups.length" class="py-4">
          <EmptyState
            :title="t('routing.noGroups')"
            :hint="t('routing.noGroupsHint')"
          />
        </div>
        <div v-else class="space-y-3">
          <div
            v-for="group in groups"
            :key="group.id || `new-${group.position}`"
            class="border p-4 transition-colors"
            :class="
              activeGroup === group
                ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
                : 'border-[var(--border-subtle)]'
            "
          >
            <div class="flex flex-wrap items-center gap-3">
              <input
                v-model="group.name"
                class="min-w-40 flex-1 border-0 border-b bg-transparent px-1 py-2 text-sm font-semibold text-[var(--text-primary)] focus:outline-none"
                style="border-color: var(--border-default)"
                :aria-label="t('routing.groupName')"
              />
              <label
                class="flex items-center gap-2 text-sm text-[var(--text-secondary)]"
              >
                <input v-model="group.enabled" type="checkbox" />
                {{ t('routing.enabled') }}
              </label>
              <label
                class="flex items-center gap-2 text-sm text-[var(--text-secondary)]"
              >
                <input
                  :checked="activeGroup === group"
                  type="radio"
                  name="active-route-group"
                  @change="activateGroup(group)"
                />
                {{ t('routing.activeGroup') }}
              </label>
              <button
                type="button"
                class="text-[var(--status-danger-text)] hover:opacity-70"
                :aria-label="t('routing.removeGroup')"
                :title="t('routing.removeGroup')"
                @click="removeGroup(group)"
              >
                <Trash2 class="h-4 w-4" aria-hidden="true" />
              </button>
            </div>

            <div v-if="activeGroup === group" class="mt-4 space-y-4">
              <div
                class="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end"
              >
                <FormField :label="t('routing.addChannel')">
                  <select
                    v-model="selectedChannelId"
                    class="h-10 w-full border bg-transparent px-3 text-sm text-[var(--text-primary)]"
                    style="border-color: var(--border-default)"
                  >
                    <option value="">{{ t('routing.selectChannel') }}</option>
                    <option
                      v-for="channel in availableChannels"
                      :key="channel.id"
                      :value="String(channel.id)"
                    >
                      #{{ channel.id }} {{ channel.name }}
                    </option>
                  </select>
                </FormField>
                <ConsoleButton
                  variant="secondary"
                  :disabled="!selectedChannelId"
                  @click="addChannel"
                >
                  <Plus class="h-4 w-4" aria-hidden="true" />
                  {{ t('common.add') }}
                </ConsoleButton>
              </div>

              <div
                class="grid gap-4 border-t pt-4 lg:grid-cols-[minmax(0,1fr)_280px]"
                style="border-color: var(--border-subtle)"
              >
                <div>
                  <p
                    class="mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]"
                  >
                    {{ t('routing.entriesTitle') }}
                  </p>
                  <div
                    v-if="!group.entries.length"
                    class="border border-dashed p-6 text-center text-sm text-[var(--text-tertiary)]"
                  >
                    {{ t('routing.noEntries') }}
                  </div>
                  <div v-else class="space-y-2">
                    <div
                      v-for="entry in group.entries"
                      :key="entry.id || `new-entry-${entry.channel_id}`"
                      draggable="true"
                      class="flex flex-wrap items-center gap-2 border p-3"
                      style="border-color: var(--border-subtle)"
                      @dragstart="draggingEntry = entry"
                      @dragover.prevent
                      @drop="dropEntry(entry)"
                    >
                      <GripVertical
                        class="h-4 w-4 shrink-0 text-[var(--text-tertiary)]"
                        aria-hidden="true"
                      />
                      <div class="min-w-36 flex-1">
                        <p
                          class="text-sm font-medium text-[var(--text-primary)]"
                        >
                          #{{ entry.channel_id }}
                          {{
                            channelFor(entry)?.name ||
                            t('routing.unavailableChannel')
                          }}
                        </p>
                        <div
                          class="mt-1 flex flex-wrap items-center gap-2 text-xs text-[var(--text-tertiary)]"
                        >
                          <StatusChip :tone="channelTone(channelFor(entry))">
                            {{
                              channelFor(entry)?.capability_state ||
                              t('routing.unavailable')
                            }}
                          </StatusChip>
                          <span v-if="channelFor(entry)?.filter_reason">{{
                            channelFor(entry)?.filter_reason
                          }}</span>
                          <span v-if="capabilityFor(entry)">
                            {{
                              capabilityFor(entry)?.lab_slug ||
                              t('routing.unknownLab')
                            }}
                          </span>
                        </div>
                      </div>
                      <label
                        class="flex items-center gap-1 text-xs text-[var(--text-secondary)]"
                      >
                        {{ t('routing.weight') }}
                        <input
                          v-model.number="entry.weight"
                          type="number"
                          min="0"
                          max="1000000"
                          class="w-20 border bg-transparent px-2 py-1 text-sm"
                          style="border-color: var(--border-default)"
                        />
                      </label>
                      <label
                        class="flex items-center gap-1 text-xs text-[var(--text-secondary)]"
                      >
                        <input v-model="entry.enabled" type="checkbox" />
                        {{ t('routing.enabled') }}
                      </label>
                      <button
                        type="button"
                        class="p-1 text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                        :aria-label="t('routing.moveUp')"
                        :title="t('routing.moveUp')"
                        @click="moveEntry(entry, -1)"
                      >
                        <ArrowUp class="h-4 w-4" aria-hidden="true" />
                      </button>
                      <button
                        type="button"
                        class="p-1 text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
                        :aria-label="t('routing.moveDown')"
                        :title="t('routing.moveDown')"
                        @click="moveEntry(entry, 1)"
                      >
                        <ArrowDown class="h-4 w-4" aria-hidden="true" />
                      </button>
                      <button
                        type="button"
                        class="p-1 text-[var(--status-danger-text)]"
                        :aria-label="t('routing.removeEntry')"
                        :title="t('routing.removeEntry')"
                        @click="removeEntry(entry)"
                      >
                        <Trash2 class="h-4 w-4" aria-hidden="true" />
                      </button>
                    </div>
                  </div>
                </div>

                <div class="space-y-3">
                  <p
                    class="text-xs font-semibold uppercase tracking-wide text-[var(--text-tertiary)]"
                  >
                    {{ t('routing.policyTitle') }}
                  </p>
                  <label
                    class="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]"
                  >
                    {{ t('routing.loadBalance') }}
                    <input
                      v-model="group.policy.load_balance"
                      type="checkbox"
                    />
                  </label>
                  <label
                    class="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]"
                  >
                    {{ t('routing.sticky') }}
                    <input v-model="group.policy.sticky" type="checkbox" />
                  </label>
                  <label
                    class="flex items-center justify-between gap-3 text-sm text-[var(--text-secondary)]"
                  >
                    {{ t('routing.maxRatio') }}
                    <input
                      v-model.number="group.policy.max_ratio"
                      type="number"
                      min="0.01"
                      max="1000"
                      step="0.01"
                      class="w-24 border bg-transparent px-2 py-1 text-sm text-[var(--text-primary)]"
                      style="border-color: var(--border-default)"
                    />
                  </label>
                  <label class="block text-sm text-[var(--text-secondary)]">
                    {{ t('routing.retryMode') }}
                    <select
                      v-model="group.policy.retry_mode"
                      class="mt-1 h-9 w-full border bg-transparent px-2 text-sm text-[var(--text-primary)]"
                      style="border-color: var(--border-default)"
                    >
                      <option value="none">none</option>
                      <option value="same_channel">same_channel</option>
                      <option value="next_channel">next_channel</option>
                      <option value="same_then_next">same_then_next</option>
                    </select>
                  </label>
                </div>
              </div>
            </div>
          </div>
        </div>
      </ConsoleCard>

      <ConsoleCard :title="t('routing.previewTitle')">
        <div v-if="!profileView" class="space-y-4">
          <p class="text-sm text-[var(--text-tertiary)]">
            {{ t('routing.createHint') }}
          </p>
          <ConsoleButton @click="save">
            <Save class="h-4 w-4" aria-hidden="true" />
            {{ t('routing.createProfile') }}
          </ConsoleButton>
        </div>
        <template v-else>
          <div class="grid gap-4 md:grid-cols-2">
            <FormField :label="t('routing.modelLabel')">
              <TextInput v-model="model" placeholder="gpt-5" />
            </FormField>
            <FormField :label="t('routing.pathLabel')">
              <TextInput v-model="path" placeholder="/v1/chat/completions" />
            </FormField>
          </div>
          <ConsoleButton
            class="mt-4"
            variant="secondary"
            :loading="loadingPreview"
            :disabled="!model.trim() || !path.trim()"
            @click="runPreview"
          >
            <RefreshCw class="h-4 w-4" aria-hidden="true" />
            {{ t('routing.runPreview') }}
          </ConsoleButton>

          <div
            v-if="preview"
            class="mt-6 space-y-4 border-t pt-4"
            style="border-color: var(--border-subtle)"
          >
            <div class="flex flex-wrap items-center gap-2 text-sm">
              <StatusChip tone="info">{{ preview.selection_mode }}</StatusChip>
              <StatusChip v-if="preview.preferred_channel_id" tone="success">{{
                t('routing.preferred', { id: preview.preferred_channel_id })
              }}</StatusChip>
              <StatusChip v-if="preview.has_mixed" tone="warning">{{
                t('routing.mixedDetected')
              }}</StatusChip>
              <span
                v-if="preview.runtime_recheck_required"
                class="text-xs text-[var(--text-tertiary)]"
                >{{ t('routing.runtimeRecheck') }}</span
              >
            </div>
            <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              <div
                v-for="entry in preview.entries"
                :key="entry.entry_id"
                class="border p-3"
                style="border-color: var(--border-subtle)"
              >
                <div class="flex items-center justify-between gap-2">
                  <span class="text-sm font-medium text-[var(--text-primary)]"
                    >#{{ entry.channel_id }}</span
                  >
                  <StatusChip
                    :tone="entry.filter_reason ? 'danger' : 'success'"
                  >
                    {{ entry.filter_reason || t('routing.eligible') }}
                  </StatusChip>
                </div>
                <p class="mt-2 text-xs text-[var(--text-tertiary)]">
                  {{ entry.lab_slug || t('routing.unknownLab') }} ·
                  {{ entry.actual_model || entry.request_model }}
                </p>
                <p class="mt-1 text-xs text-[var(--text-tertiary)]">
                  snapshot {{ entry.snapshot_version }} ·
                  {{ entry.catalog_version || '-' }}
                </p>
                <div class="mt-2 flex flex-wrap items-center gap-2 text-xs">
                  <StatusChip :tone="healthTone(entry.health.state)">
                    {{ t(`routing.healthState.${entry.health.state}`) }}
                  </StatusChip>
                  <span
                    class="text-[var(--text-tertiary)]"
                    data-testid="route-preview-health"
                  >
                    {{
                      t('routing.healthSummary', {
                        failures: entry.health.failure_count,
                        latency: entry.health.last_latency_ms,
                      })
                    }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </template>
      </ConsoleCard>
    </template>
  </div>
</template>
