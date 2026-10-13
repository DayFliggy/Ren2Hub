<script setup lang="ts">
/**
 * Vendor create/edit drawer (NewAPI vendor-mutate-dialog + vendor-linked-models).
 * Mounted with v-if; leaving with unsaved details always asks for confirmation.
 */
import { computed, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import {
  metadataApi,
  type ModelMetadata,
  type ModelVendor,
} from '@/api/adminManagement'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FormField from '@/components/common/FormField.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { adminManagementMessages } from '@/i18n/adminManagement'
import { formatTime } from '@/utils/format'
import MetadataIcon from './MetadataIcon.vue'

type LinkedRow = ModelMetadata & Record<string, unknown>

const props = defineProps<{
  vendor: ModelVendor | null
  linkedCount: number
}>()
const emit = defineEmits<{
  close: []
  saved: []
  'open-model': [payload: { id: number }]
}>()
const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
const router = useRouter()
const toast = useToast()
const panelId = useId()

const form = reactive({
  name: props.vendor?.name ?? '',
  description: props.vendor?.description ?? '',
  icon: props.vendor?.icon ?? '',
})
const snapshot = () => JSON.stringify(form)
const initial = ref(snapshot())
const dirty = computed(() => snapshot() !== initial.value)
const section = ref<'details' | 'models'>('details')
const tabs = computed(() => [
  { key: 'details', label: t('vendorDetails') },
  { key: 'models', label: `${t('linkedModels')} (${props.linkedCount})` },
])
const saving = ref(false)
const saveError = ref('')
const pendingLeave = ref<(() => void) | null>(null)
const rules = ['exact', 'prefix', 'contains', 'suffix'] as const
useUnsavedChanges({
  dirty: () => dirty.value,
  busy: () => saving.value,
  message: () => t('discardMessage'),
})

async function save() {
  if (saving.value) return
  saveError.value = ''
  const name = form.name.trim()
  if (!name) {
    saveError.value = t('vendorNameRequired')
    return
  }
  saving.value = true
  try {
    // Status has no control here (as upstream): keep the stored value.
    await metadataApi.saveVendor({
      ...(props.vendor ? { id: props.vendor.id } : {}),
      name,
      description: form.description,
      icon: form.icon,
      status: props.vendor?.status ?? 1,
    })
    form.name = name
    initial.value = snapshot()
    toast.success(t(props.vendor ? 'vendorUpdated' : 'vendorCreated'))
    emit('saved')
  } catch (cause) {
    saveError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}

/** Runs a leave action, asking first when the details form has edits. */
function leave(action: () => void) {
  if (saving.value) return
  if (!dirty.value) {
    action()
    return
  }
  pendingLeave.value = action
}
function discard() {
  const action = pendingLeave.value
  pendingLeave.value = null
  initial.value = snapshot()
  action?.()
}
const requestClose = () => leave(() => emit('close'))
function openFilteredModels(id: number) {
  leave(() => {
    emit('close')
    void router.push({
      path: '/models/metadata',
      query: { vendor: String(id) },
    })
  })
}
const openModel = (id: number) => leave(() => emit('open-model', { id }))

const linked = useAdminRequest()
const linkedRows = ref<LinkedRow[]>([])
const linkedTotal = ref(0)
const linkedPage = ref(1)
const linkedPageSize = ref(10)
const linkedColumns = computed<TableColumn[]>(() => [
  { key: 'model_name', label: t('model'), width: '220px' },
  { key: 'name_rule', label: t('nameRule'), width: '140px' },
  { key: 'matched_count', label: t('matchedModels'), width: '120px' },
])

async function loadLinked() {
  const vendor = props.vendor
  if (!vendor) return
  await linked.run(async (signal) => {
    const result = await metadataApi.models(
      {
        vendor: vendor.id,
        p: linkedPage.value,
        page_size: linkedPageSize.value,
      },
      signal
    )
    if (signal.aborted) return
    linkedRows.value = result.items as LinkedRow[]
    linkedTotal.value = result.total
  })
}
watch([section, linkedPage, linkedPageSize], () => {
  saveError.value = ''
  if (section.value === 'models') void loadLinked()
})

const ruleLabel = (rule: number) => t(rules[rule] ?? 'exact')
const matchedLabel = (row: ModelMetadata) =>
  row.name_rule === 0 ? '—' : String(row.matched_count)
const timeLabel = (value: number | null) => (value ? formatTime(value) : '—')
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="vendor ? vendor.name : t('createVendor')"
    :subtitle="t('vendorSubtitle')"
    presentation="drawer"
    size="lg"
    :close-disabled="saving"
    @close="requestClose"
  >
    <ConsoleTabs
      v-if="vendor"
      v-model="section"
      :items="tabs"
      :panel-id="panelId"
      class="-mt-2 mb-6"
    />
    <div
      :id="panelId"
      :role="vendor ? 'tabpanel' : undefined"
      :aria-labelledby="vendor ? `${panelId}-tab-${section}` : undefined"
    >
      <form
        v-if="section === 'details'"
        id="vendor-drawer-form"
        class="sibling-space-y-6"
        novalidate
        @submit.prevent="save"
      >
        <fieldset class="min-w-0 sibling-space-y-6" :disabled="saving">
          <FormField :label="t('vendorName')">
            <TextInput
              v-model="form.name"
              name="vendor-name"
              maxlength="128"
              required
            />
          </FormField>
          <FormField :label="t('description')">
            <textarea
              v-model="form.description"
              name="vendor-description"
              rows="4"
              class="w-full resize-y rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3 text-sm text-[var(--text-primary)] outline-hidden transition focus:border-[var(--accent)] focus-ring"
            />
          </FormField>
          <FormField :label="t('icon')">
            <div class="flex items-center gap-3">
              <TextInput
                v-model="form.icon"
                name="vendor-icon"
                maxlength="128"
                class="min-w-0 flex-1"
              />
              <MetadataIcon
                :icon="form.icon"
                :name="form.name || vendor?.name || ''"
                :size="36"
              />
            </div>
          </FormField>
        </fieldset>
        <div
          v-if="vendor"
          class="sibling-space-y-1 border-t border-[var(--border-subtle)] pt-3 text-xs text-[var(--text-tertiary)]"
        >
          <p>{{ t('createdAt') }}: {{ timeLabel(vendor.created_time) }}</p>
          <p>{{ t('updatedAt') }}: {{ timeLabel(vendor.updated_time) }}</p>
        </div>
        <p
          v-if="saveError"
          role="alert"
          class="text-sm text-[var(--status-danger-text)]"
        >
          {{ saveError }}
        </p>
      </form>
      <div v-else-if="vendor" class="sibling-space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <p class="text-sm text-[var(--text-secondary)]">
            {{ t('linkedRecords', { count: linkedCount }) }}
          </p>
          <ConsoleButton
            variant="secondary"
            size="sm"
            @click="openFilteredModels(vendor.id)"
          >
            {{ t('openFilteredModels') }}
          </ConsoleButton>
        </div>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('linkedHint') }}
        </p>
        <ErrorBanner
          v-if="linked.error.value"
          :message="linked.error.value"
          @retry="loadLinked"
        />
        <DataTable
          v-else
          :columns="linkedColumns"
          :rows="linkedRows"
          row-key="id"
          :loading="linked.loading.value"
          :skeleton-rows="linkedPageSize"
          min-table-width="480px"
          :scroll-region-label="t('linkedModels')"
          :empty-title="t('noLinkedModels')"
          :empty-hint="t('noLinkedModelsHint')"
        >
          <template #cell-model_name="{ row }">
            <button
              type="button"
              class="max-w-full truncate text-left font-mono text-sm text-[var(--text-primary)] underline-offset-4 hover:underline focus-ring"
              :title="(row as LinkedRow).model_name"
              @click="openModel((row as LinkedRow).id)"
            >
              {{ (row as LinkedRow).model_name }}
            </button>
          </template>
          <template #cell-name_rule="{ row }">
            <span class="text-xs text-[var(--text-secondary)]">
              {{ ruleLabel((row as LinkedRow).name_rule) }}
            </span>
          </template>
          <template #cell-matched_count="{ row }">
            <span class="text-xs tabular-nums text-[var(--text-secondary)]">
              {{ matchedLabel(row as LinkedRow) }}
            </span>
          </template>
          <template #footer>
            <TablePagination
              v-model:page="linkedPage"
              v-model:page-size="linkedPageSize"
              :total="linkedTotal"
            />
          </template>
        </DataTable>
      </div>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton variant="ghost" :disabled="saving" @click="requestClose">
          {{ t('close') }}
        </ConsoleButton>
        <ConsoleButton
          v-if="section === 'details'"
          type="submit"
          form="vendor-drawer-form"
          :loading="saving"
        >
          {{ saving ? t('saving') : t('saveMetadata') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
  <ConfirmDialog
    :open="pendingLeave !== null"
    :title="t('discardTitle')"
    :message="t('discardMessage')"
    :confirm-text="t('discardConfirm')"
    @confirm="discard"
    @cancel="pendingLeave = null"
  />
</template>
