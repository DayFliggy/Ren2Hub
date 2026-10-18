<script setup lang="ts">
/** One NewAPI model-management column for a metadata row. */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy } from 'lucide-vue-next'
import type { ModelMetadata, ModelVendor } from '@/api/adminManagement'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { adminChannelTypeMeta } from '@/constants/adminChannels'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import { parseModelEndpoints } from '@/utils/modelEndpoints'
import MetadataIcon from './MetadataIcon.vue'
import { parseMetadataTags } from './modelMetadataForm'

export type ModelMetadataField =
  | 'model'
  | 'connections'
  | 'tags'
  | 'sync_official'
  | 'status'
  | 'id'
  | 'vendor'
  | 'name_rule'
  | 'description'
  | 'endpoints'
  | 'created_time'
  | 'updated_time'

const props = defineProps<{
  row: ModelMetadata
  field: ModelMetadataField
  vendor?: ModelVendor
}>()
const emit = defineEmits<{ copy: [name: string]; edit: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMetadataMessages,
})
const RULES = ['exact', 'prefix', 'contains', 'suffix'] as const
const tagsOpen = ref(false)

const tags = computed(() => parseMetadataTags(props.row.tags))
// Only explicitly configured endpoints are "custom"; the API returns a plain
// type array when it inferred them from channels.
const endpointKeys = computed(() => {
  if (!props.row.endpoints.trim()) return []
  try {
    const value = parseModelEndpoints(props.row.endpoints)
    return Array.isArray(value) ? [] : Object.keys(value)
  } catch {
    return []
  }
})
const visibleTags = computed(() =>
  tagsOpen.value ? tags.value : tags.value.slice(0, 1)
)
const ruleLabel = computed(() => t(RULES[props.row.name_rule] ?? 'exact'))
const connectionsTitle = computed(() =>
  [
    ...props.row.bound_channels.map(
      (channel) =>
        `${channel.name} (${adminChannelTypeMeta(channel.type).label})`
    ),
  ].join('\n')
)

function formatTime(value: number | null) {
  if (value == null || value <= 0) return '—'
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value * 1000))
}
</script>
<template>
  <div v-if="field === 'model'" class="flex min-w-0 items-center gap-2.5">
    <MetadataIcon
      :icon="row.icon || vendor?.icon"
      :name="vendor?.name || row.model_name"
      :size="24"
    />
    <div class="min-w-0">
      <div class="flex min-w-0 items-center gap-1">
        <button
          type="button"
          class="min-w-0 truncate font-mono text-sm font-semibold text-[var(--text-primary)] underline-offset-4 hover:underline focus-ring"
          :title="row.model_name"
          @click="emit('edit')"
        >
          {{ row.model_name }}
        </button>
        <IconButton
          :label="t('copyModelName')"
          class="shrink-0"
          @click="emit('copy', row.model_name)"
          ><Copy :size="13"
        /></IconButton>
      </div>
      <p class="truncate text-xs text-[var(--text-tertiary)]">
        {{ vendor?.name || t('noVendor')
        }}<template v-if="row.name_rule !== 0">
          ·
          {{ t('ruleMatches', { rule: ruleLabel, count: row.matched_count }) }}
        </template>
      </p>
    </div>
  </div>
  <span
    v-else-if="field === 'connections'"
    class="text-xs tabular-nums text-[var(--text-secondary)]"
    :title="connectionsTitle || undefined"
    >{{
      t('connectionsSummary', {
        channels: row.bound_channels.length,
      })
    }}</span
  >
  <div
    v-else-if="field === 'tags'"
    class="flex min-w-0 flex-wrap items-center gap-1"
  >
    <StatusChip v-for="(tag, index) in visibleTags" :key="index">{{
      tag
    }}</StatusChip>
    <button
      v-if="tags.length > 1"
      type="button"
      class="rounded-[var(--chip-radius)] px-1.5 text-xs text-[var(--text-tertiary)] hover:text-[var(--text-primary)] focus-ring"
      :aria-expanded="tagsOpen"
      @click="tagsOpen = !tagsOpen"
    >
      {{ tagsOpen ? '−' : t('moreTags', { count: tags.length - 1 }) }}
    </button>
    <span v-if="!tags.length" class="text-xs text-[var(--text-tertiary)]"
      >—</span
    >
  </div>
  <span
    v-else-if="field === 'sync_official'"
    class="text-xs text-[var(--text-tertiary)]"
    >{{ t(row.sync_official === 1 ? 'allowUpdates' : 'keepLocal') }}</span
  >
  <StatusChip
    v-else-if="field === 'status'"
    :tone="row.status === 1 ? 'success' : 'neutral'"
    >{{ t(row.status === 1 ? 'allowed' : 'notListed') }}</StatusChip
  >
  <span
    v-else-if="field === 'id'"
    class="font-mono text-xs tabular-nums text-[var(--text-secondary)]"
    >{{ row.id }}</span
  >
  <div v-else-if="field === 'vendor'" class="flex min-w-0 items-center gap-2">
    <MetadataIcon
      v-if="vendor"
      :icon="vendor.icon"
      :name="vendor.name"
      :size="18"
    /><span class="truncate text-xs text-[var(--text-secondary)]">{{
      vendor?.name || t('noVendor')
    }}</span>
  </div>
  <span
    v-else-if="field === 'name_rule'"
    class="text-xs text-[var(--text-secondary)]"
    >{{ ruleLabel }}</span
  >
  <span
    v-else-if="field === 'description'"
    class="line-clamp-2 text-xs text-[var(--text-secondary)]"
    :title="row.description || undefined"
    >{{ row.description || '—' }}</span
  >
  <div
    v-else-if="field === 'endpoints'"
    class="flex min-w-0 flex-wrap items-center gap-1"
  >
    <StatusChip v-for="key in endpointKeys" :key="key">{{ key }}</StatusChip>
    <span
      v-if="!endpointKeys.length"
      class="text-xs text-[var(--text-tertiary)]"
      >—</span
    >
  </div>
  <span v-else class="text-xs tabular-nums text-[var(--text-secondary)]">{{
    formatTime(row[field as 'created_time' | 'updated_time'])
  }}</span>
</template>
