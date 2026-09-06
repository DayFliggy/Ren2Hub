<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy } from 'lucide-vue-next'
import type { ModelMetadata, ModelVendor } from '@/api/adminManagement'
import MetadataIcon from './MetadataIcon.vue'
import IconButton from '@/components/common/IconButton.vue'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import { parseMetadataTags } from './modelMetadataForm'
import { parseModelEndpoints } from '@/utils/modelEndpoints'

const props = defineProps<{
  row: ModelMetadata
  field: string
  vendor?: ModelVendor
}>()
const emit = defineEmits<{ copy: [name: string] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMetadataMessages,
})
const ruleNames = ['exact', 'prefix', 'contains', 'suffix']
const tags = computed(() => {
  try {
    return { items: parseMetadataTags(props.row.tags), invalid: false }
  } catch {
    return { items: [props.row.tags], invalid: true }
  }
})
const endpoints = computed(() => {
  if (!props.row.endpoints) return { items: [], invalid: false }
  try {
    const value = parseModelEndpoints(props.row.endpoints)
    return {
      items: Array.isArray(value)
        ? value
        : Object.entries(value).map(
            ([type, item]) => `${type}: ${item.method} ${item.path}`
          ),
      invalid: false,
    }
  } catch {
    return { items: [props.row.endpoints], invalid: true }
  }
})
const items = computed(() => {
  switch (props.field) {
    case 'tags':
      return tags.value.items
    case 'endpoints':
      return endpoints.value.items
    case 'bound_channels':
      return props.row.bound_channels.map(
        (channel) => `${channel.name} (${channel.type})`
      )
    case 'enable_groups':
      return props.row.enable_groups
    case 'quota_types':
      return props.row.quota_types.map((type) =>
        type === 0 ? t('usage') : type === 1 ? t('perCall') : String(type)
      )
    default:
      return []
  }
})
function formatTime(value: number | null) {
  return value == null || value <= 0
    ? t('unknown')
    : new Intl.DateTimeFormat(locale.value, {
        dateStyle: 'medium',
        timeStyle: 'short',
      }).format(new Date(value * 1000))
}
</script>
<template>
  <div v-if="field === 'model_name'" class="metadata-name">
    <MetadataIcon
      :icon="row.icon || vendor?.icon"
      :name="vendor?.name || row.model_name"
      :size="28"
    /><strong>{{ row.model_name }}</strong
    ><IconButton :label="t('copy')" @click="emit('copy', row.model_name)"
      ><Copy :size="14"
    /></IconButton>
  </div>
  <template v-else-if="field === 'id'">{{ row.id }}</template>
  <span
    v-else-if="field === 'status'"
    class="metadata-badge"
    :class="row.status === 1 ? 'metadata-positive' : ''"
    >{{ t(row.status === 1 ? 'enabled' : 'disabled') }}</span
  >
  <span
    v-else-if="field === 'sync_official'"
    class="metadata-badge"
    :class="row.sync_official === 1 ? 'metadata-positive' : ''"
    >{{ t(row.sync_official === 1 ? 'syncEnabled' : 'syncDisabled') }}</span
  >
  <div v-else-if="field === 'vendor_id'" class="metadata-name">
    <MetadataIcon
      v-if="vendor"
      :icon="vendor.icon"
      :name="vendor.name"
      :size="22"
    /><span>{{ vendor?.name || t('noVendor') }}</span>
  </div>
  <div v-else-if="field === 'name_rule'">
    <span class="metadata-badge">{{ t(ruleNames[row.name_rule]) }}</span>
    <details v-if="row.name_rule !== 0" class="metadata-details">
      <summary>{{ t('matchCount', { count: row.matched_count }) }}</summary>
      <ul>
        <li v-for="name in row.matched_models" :key="name">{{ name }}</li>
      </ul>
    </details>
  </div>
  <template v-else-if="field === 'created_time' || field === 'updated_time'">{{
    formatTime(row[field])
  }}</template>
  <details
    v-else-if="field === 'description' && row.description"
    class="metadata-description"
  >
    <summary>{{ row.description }}</summary>
    <p>{{ row.description }}</p>
  </details>
  <template v-else-if="field === 'description'">{{ t('unknown') }}</template>
  <div v-else class="metadata-values">
    <span v-if="field === 'tags' && tags.invalid" class="admin-error">{{
      t('invalidTags')
    }}</span>
    <span
      v-if="field === 'endpoints' && endpoints.invalid"
      class="admin-error"
      >{{ t('endpointInvalidJson') }}</span
    >
    <span v-for="item in items" :key="item" class="metadata-badge">{{
      item
    }}</span
    ><span v-if="!items.length" class="admin-muted">{{ t('unknown') }}</span>
  </div>
</template>
<style scoped>
.metadata-name {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  min-width: 0;
}
.metadata-name strong {
  overflow-wrap: anywhere;
  font-size: 0.875rem;
}
.metadata-name button {
  flex-shrink: 0;
}
.metadata-values {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}
.metadata-badge {
  display: inline-block;
  max-width: 100%;
  padding: 0.2rem 0.45rem;
  border-radius: var(--shape-small);
  font-size: 0.75rem;
  background: var(--surface-container-high);
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.metadata-positive {
  background: var(--status-success-soft);
  color: var(--status-success-text);
}
.metadata-details {
  margin-top: 0.35rem;
  font-size: 0.75rem;
}
.metadata-details ul {
  max-height: 10rem;
  overflow-y: auto;
  padding-top: 0.5rem;
}
.metadata-description summary {
  max-width: 16rem;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  cursor: pointer;
}
.metadata-description p {
  min-width: 12rem;
  max-width: 24rem;
  white-space: pre-wrap;
  margin-top: 0.5rem;
}
summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
</style>
