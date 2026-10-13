<script setup lang="ts">
/**
 * Prefill group management (NewAPI prefill-group-management): a list dialog
 * that swaps to a form drawer for create/edit, so the two never stack.
 * List items must be non-empty and unique; endpoint JSON goes through
 * parseModelEndpoints before saving.
 */
import { Layers3, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  metadataApi,
  type PrefillGroup,
  type PrefillInput,
} from '@/api/adminManagement'
import ActionMenu, {
  type ActionMenuItem,
} from '@/components/common/ActionMenu.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import DataTable, { type TableColumn } from '@/components/common/DataTable.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import IconButton from '@/components/common/IconButton.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { adminManagementMessages } from '@/i18n/adminManagement'
import {
  MODEL_ENDPOINT_TEMPLATES,
  parseModelEndpoints,
} from '@/utils/modelEndpoints'
import StringListEditor from './StringListEditor.vue'

type GroupType = PrefillGroup['type']
interface GroupRow extends Record<string, unknown> {
  id: number
  group: PrefillGroup
  items: string[]
}

const PREVIEW_LIMIT = 6
// Upstream colours the types blue/purple/cyan; local chips stay restrained.
const TYPE_META: Record<
  GroupType,
  { label: string; hint: string; tone: 'accent' | 'info' | 'neutral' }
> = {
  model: { label: 'modelGroup', hint: 'modelGroupHint', tone: 'accent' },
  tag: { label: 'tagGroup', hint: 'tagGroupHint', tone: 'neutral' },
  endpoint: {
    label: 'endpointGroup',
    hint: 'endpointGroupHint',
    tone: 'info',
  },
}
const GROUP_TYPES = Object.keys(TYPE_META) as GroupType[]

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
const toast = useToast()
const { loading, error, run } = useAdminRequest()

const view = ref<'list' | 'form'>('list')
const groups = ref<PrefillGroup[]>([])

/** Display names for an item column: list entries or endpoint keys. */
function itemNames(group: PrefillGroup): string[] {
  if (group.type !== 'endpoint') return group.items
  const endpoints = parseModelEndpoints(group.items)
  return Array.isArray(endpoints) ? endpoints : Object.keys(endpoints)
}
const rows = computed<GroupRow[]>(() =>
  [...groups.value]
    .sort((a, b) =>
      a.type === b.type
        ? a.name.localeCompare(b.name)
        : a.type.localeCompare(b.type)
    )
    .map((group) => ({ id: group.id, group, items: itemNames(group) }))
)
const columns = computed<TableColumn[]>(() => [
  { key: 'group', label: t('group'), width: '220px' },
  { key: 'type', label: t('type'), width: '120px' },
  { key: 'items', label: t('items'), width: '260px' },
  { key: 'actions', label: t('actions'), width: '96px', align: 'right' },
])
const rowMenu = computed<ActionMenuItem[]>(() => [
  { key: 'delete', label: t('deleteGroup'), icon: Trash2, tone: 'danger' },
])

async function load() {
  await run(async (signal) => {
    const result = await metadataApi.groups(signal)
    if (!signal.aborted) groups.value = result
  })
}
watch(
  () => props.open && view.value === 'list',
  (showList) => {
    if (showList) void load()
  },
  { immediate: true }
)

const typeLabel = (type: GroupType) => t(TYPE_META[type].label)
const typeOptions = computed(() =>
  GROUP_TYPES.map((type) => ({ value: type, label: typeLabel(type) }))
)
const itemCount = (count: number) => t('itemCount', { count }, count)
const emptyItems = (group: PrefillGroup) =>
  t(group.type === 'endpoint' ? 'noEndpointItems' : 'noListItems')

const form = reactive({
  id: undefined as number | undefined,
  name: '',
  description: '',
  type: 'model' as GroupType,
  list: [] as string[],
  json: '{}',
})
const saving = ref(false)
const formError = ref('')
const baseline = ref('')
const dirty = computed(
  () =>
    props.open &&
    view.value === 'form' &&
    JSON.stringify(form) !== baseline.value
)
const typeModel = computed({
  get: () => form.type,
  set: (value: string) => {
    form.type = value as GroupType
  },
})

function openForm(group?: PrefillGroup) {
  Object.assign(form, {
    id: group?.id,
    name: group?.name ?? '',
    description: group?.description ?? '',
    type: group?.type ?? 'model',
    list: group && group.type !== 'endpoint' ? [...group.items] : [],
    json: group?.type === 'endpoint' ? group.items : '{}',
  })
  formError.value = ''
  baseline.value = JSON.stringify(form)
  view.value = 'form'
}
function closeForm() {
  if (saving.value || (dirty.value && !window.confirm(t('discardMessage'))))
    return
  view.value = 'list'
}
function fillTemplate() {
  form.json = JSON.stringify(MODEL_ENDPOINT_TEMPLATES, null, 2)
}

/** Builds the request body; throws a localized message on invalid input. */
function groupInput(): PrefillInput {
  const name = form.name.trim()
  if (!name) throw new Error(t('groupNameRequired'))
  const base = { id: form.id, name, description: form.description }
  if (form.type === 'endpoint') {
    let endpoints
    try {
      endpoints = parseModelEndpoints(form.json)
    } catch {
      throw new Error(t('invalidEndpoint'))
    }
    return { ...base, type: 'endpoint', items: JSON.stringify(endpoints) }
  }
  const items = form.list.map((item) => item.trim())
  if (items.some((item) => !item) || new Set(items).size !== items.length)
    throw new Error(t('invalidList'))
  return { ...base, type: form.type, items }
}
async function save() {
  if (saving.value) return
  formError.value = ''
  saving.value = true
  try {
    await metadataApi.saveGroup(groupInput())
    toast.success(t(form.id ? 'groupUpdated' : 'groupCreated'))
    emit('saved')
    view.value = 'list'
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}

const deleting = ref<PrefillGroup | null>(null)
const deleteError = ref('')
const deleteBusy = ref(false)
useUnsavedChanges({
  dirty: () => dirty.value,
  busy: () => props.open && (saving.value || deleteBusy.value),
  message: () => t('discardMessage'),
  leaves: (to, from) =>
    props.open &&
    (to.path !== from.path || to.query.manage !== from.query.manage),
})
function requestDelete(group: PrefillGroup) {
  deleting.value = group
  deleteError.value = ''
}
function cancelDelete() {
  if (!deleteBusy.value) deleting.value = null
}
async function confirmDelete() {
  const group = deleting.value
  if (!group || deleteBusy.value) return
  deleteBusy.value = true
  deleteError.value = ''
  try {
    await metadataApi.deleteGroup(group.id)
    deleting.value = null
    toast.success(t('deleted', { name: group.name }))
    emit('saved')
    void load()
  } catch (cause) {
    deleteError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    deleteBusy.value = false
  }
}
watch(
  () => props.open,
  (open) => {
    if (open) return
    view.value = 'list'
    deleting.value = null
  }
)
</script>

<template>
  <ConsoleModal
    :open="open && view === 'list'"
    :aria-label="t('prefillTitle')"
    size="xl"
    @close="emit('close')"
  >
    <header class="mb-5">
      <h2
        class="display-title flex items-center gap-2 text-xl font-bold text-[var(--text-primary)]"
      >
        <Layers3 :size="20" class="text-[var(--text-secondary)]" />
        {{ t('prefillTitle') }}
      </h2>
      <p class="mt-1.5 text-sm leading-relaxed text-[var(--text-tertiary)]">
        {{ t('prefillSubtitle') }}
      </p>
    </header>
    <div
      class="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2"
    >
      <div class="flex flex-wrap items-center gap-2">
        <ConsoleButton size="sm" @click="openForm()">
          <Plus :size="14" aria-hidden="true" />
          {{ t('newGroup') }}
        </ConsoleButton>
        <ConsoleButton
          size="sm"
          variant="ghost"
          :loading="loading"
          @click="load"
        >
          <RefreshCw v-if="!loading" :size="14" aria-hidden="true" />
          {{ t('refresh') }}
        </ConsoleButton>
      </div>
      <span class="text-xs text-[var(--text-tertiary)]" aria-live="polite">
        {{ t('groupCount', { count: groups.length }, groups.length) }}
      </span>
    </div>

    <ErrorBanner v-if="error" :message="error" @retry="load" />
    <template v-else>
      <DataTable
        class="hidden md:block"
        :columns="columns"
        :rows="rows"
        row-key="id"
        :loading="loading"
        :skeleton-rows="4"
        min-table-width="680px"
        :scroll-region-label="t('prefillTitle')"
        :empty-title="t('noGroups')"
        :empty-hint="t('noGroupsHint')"
      >
        <template #cell-group="{ row }">
          <div class="flex min-w-0 flex-col gap-1 whitespace-normal">
            <div class="flex flex-wrap items-center gap-2">
              <span class="font-mono text-sm text-[var(--text-primary)]">{{
                (row as GroupRow).group.name
              }}</span>
              <span class="font-mono text-xs text-[var(--text-tertiary)]"
                >#{{ (row as GroupRow).id }}</span
              >
            </div>
            <p
              v-if="(row as GroupRow).group.description"
              class="text-xs text-[var(--text-tertiary)]"
            >
              {{ (row as GroupRow).group.description }}
            </p>
            <p v-else class="text-xs italic text-[var(--text-tertiary)]">
              {{ t('noDescription') }}
            </p>
          </div>
        </template>
        <template #cell-type="{ row }">
          <StatusChip :tone="TYPE_META[(row as GroupRow).group.type].tone">
            {{ typeLabel((row as GroupRow).group.type) }}
          </StatusChip>
        </template>
        <template #cell-items="{ row }">
          <div
            v-if="(row as GroupRow).items.length"
            class="flex flex-wrap gap-1.5"
          >
            <StatusChip
              v-for="item in (row as GroupRow).items.slice(0, PREVIEW_LIMIT)"
              :key="item"
              class="max-w-full truncate font-mono"
              :title="item"
            >
              {{ item }}
            </StatusChip>
            <StatusChip v-if="(row as GroupRow).items.length > PREVIEW_LIMIT">
              +{{ (row as GroupRow).items.length - PREVIEW_LIMIT }}
            </StatusChip>
          </div>
          <p v-else class="text-xs text-[var(--text-tertiary)]">
            {{ emptyItems((row as GroupRow).group) }}
          </p>
          <p class="mt-2 text-xs text-[var(--text-tertiary)]">
            {{ itemCount((row as GroupRow).items.length) }}
          </p>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center justify-end gap-0.5">
            <IconButton
              :label="t('editGroup')"
              @click="openForm((row as GroupRow).group)"
            >
              <Pencil :size="15" />
            </IconButton>
            <ActionMenu
              :items="rowMenu"
              :label="t('openMenu')"
              @select="requestDelete((row as GroupRow).group)"
            />
          </div>
        </template>
      </DataTable>

      <div class="md:hidden">
        <div v-if="loading" class="sibling-space-y-3" aria-hidden="true">
          <div
            v-for="i in 3"
            :key="i"
            class="sibling-space-y-3 rounded-xl border border-[var(--border-subtle)] p-4"
          >
            <SkeletonBlock rounded="md" class="h-4 w-1/2" />
            <SkeletonBlock rounded="md" class="h-10" />
          </div>
        </div>
        <EmptyState
          v-else-if="rows.length === 0"
          :title="t('noGroups')"
          :hint="t('noGroupsHint')"
          illustration="empty-search"
        />
        <div v-else class="sibling-space-y-3">
          <article
            v-for="row in rows"
            :key="row.id"
            class="rounded-xl border border-[var(--border-subtle)] p-4"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 sibling-space-y-1.5">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-mono text-sm text-[var(--text-primary)]">{{
                    row.group.name
                  }}</span>
                  <StatusChip :tone="TYPE_META[row.group.type].tone">
                    {{ typeLabel(row.group.type) }}
                    <span class="font-mono opacity-70">#{{ row.id }}</span>
                  </StatusChip>
                </div>
                <p
                  class="line-clamp-2 text-xs text-[var(--text-tertiary)]"
                  :class="{ italic: !row.group.description }"
                >
                  {{ row.group.description || t('noDescription') }}
                </p>
              </div>
              <div class="flex shrink-0 items-center gap-0.5">
                <IconButton
                  :label="t('editGroup')"
                  @click="openForm(row.group)"
                >
                  <Pencil :size="15" />
                </IconButton>
                <IconButton
                  :label="t('deleteGroup')"
                  tone="danger"
                  @click="requestDelete(row.group)"
                >
                  <Trash2 :size="15" />
                </IconButton>
              </div>
            </div>
            <p class="mt-3 text-xs text-[var(--text-tertiary)]">
              {{ t('items') }} · {{ itemCount(row.items.length) }}
            </p>
            <div v-if="row.items.length" class="mt-2 flex flex-wrap gap-1.5">
              <StatusChip
                v-for="item in row.items.slice(0, PREVIEW_LIMIT)"
                :key="item"
                class="max-w-full truncate font-mono"
                :title="item"
              >
                {{ item }}
              </StatusChip>
              <StatusChip v-if="row.items.length > PREVIEW_LIMIT">
                +{{ row.items.length - PREVIEW_LIMIT }}
              </StatusChip>
            </div>
            <p v-else class="mt-2 text-xs text-[var(--text-tertiary)]">
              {{ emptyItems(row.group) }}
            </p>
          </article>
        </div>
      </div>
    </template>
    <template #footer>
      <div class="flex justify-end">
        <ConsoleButton variant="ghost" @click="emit('close')">
          {{ t('close') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
  <ConsoleModal
    :open="open && view === 'form'"
    :title="form.id ? t('editGroupTitle') : t('createGroup')"
    :subtitle="form.id ? t('editGroupSubtitle') : t('createGroupSubtitle')"
    presentation="drawer"
    size="lg"
    :close-disabled="saving"
    @close="closeForm"
  >
    <form
      id="prefill-group-form"
      class="sibling-space-y-6"
      novalidate
      @submit.prevent="save"
    >
      <fieldset class="min-w-0 sibling-space-y-6" :disabled="saving">
        <section class="sibling-space-y-4">
          <div>
            <h3 class="text-sm font-semibold text-[var(--text-secondary)]">
              {{ t('groupDetails') }}
            </h3>
            <p class="mt-1 text-xs text-[var(--text-tertiary)]">
              {{ t('groupDetailsHint') }}
            </p>
          </div>
          <FormField :label="t('groupName')" :hint="t('groupNameHint')">
            <TextInput
              v-model="form.name"
              name="prefill-group-name"
              maxlength="64"
              required
              :placeholder="t('groupNamePlaceholder')"
            />
          </FormField>
          <FormField
            :label="t('description')"
            :hint="t('groupDescriptionHint')"
          >
            <textarea
              v-model="form.description"
              name="prefill-group-description"
              rows="3"
              maxlength="255"
              :placeholder="t('groupDescriptionPlaceholder')"
              class="w-full resize-y rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] outline-hidden transition focus:border-[var(--accent)] focus-ring"
            />
          </FormField>
        </section>

        <section
          class="sibling-space-y-4 border-t border-[var(--border-subtle)] pt-6"
        >
          <div>
            <h3 class="text-sm font-semibold text-[var(--text-secondary)]">
              {{ t('configuration') }}
            </h3>
            <p class="mt-1 text-xs text-[var(--text-tertiary)]">
              {{ t('configurationHint') }}
            </p>
          </div>
          <!-- A div, not FormField's <label>: the select and list editor
               contain their own buttons, which a label would re-activate. -->
          <div>
            <span
              class="mb-1.5 block text-sm font-medium text-[var(--text-secondary)]"
              >{{ t('groupType') }}</span
            >
            <FilterSelect
              v-model="typeModel"
              :options="typeOptions"
              :label="t('groupType')"
              class="h-11"
            />
            <p class="mt-1.5 text-xs text-[var(--text-tertiary)]">
              {{ t(TYPE_META[form.type].hint) }}
            </p>
            <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
              {{ t('groupTypeHint') }}
            </p>
          </div>
          <div
            class="sibling-space-y-3 border-y border-[var(--border-subtle)] py-4"
          >
            <div class="flex flex-wrap items-center gap-2">
              <h4 class="text-sm font-medium text-[var(--text-primary)]">
                {{ t('items') }}
              </h4>
              <StatusChip :tone="TYPE_META[form.type].tone">
                {{ typeLabel(form.type) }}
              </StatusChip>
              <button
                v-if="form.type === 'endpoint'"
                type="button"
                class="ml-auto text-xs font-medium text-[var(--accent-text)] underline-offset-4 hover:underline focus-ring"
                @click="fillTemplate"
              >
                {{ t('fillTemplate') }}
              </button>
            </div>
            <template v-if="form.type === 'endpoint'">
              <textarea
                v-model="form.json"
                name="prefill-group-endpoints"
                rows="12"
                spellcheck="false"
                :aria-label="t('items')"
                class="w-full resize-y rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3 font-mono text-xs text-[var(--text-primary)] outline-hidden transition focus:border-[var(--accent)] focus-ring"
              />
              <p class="text-xs text-[var(--text-tertiary)]">
                {{ t('endpointItemsHint') }}
              </p>
            </template>
            <template v-else>
              <StringListEditor v-model="form.list" :label="t('items')" />
              <p class="text-xs text-[var(--text-tertiary)]">
                {{ t('listItemsHint') }}
              </p>
            </template>
          </div>
        </section>
      </fieldset>
      <p
        v-if="formError"
        role="alert"
        class="text-sm text-[var(--status-danger-text)]"
      >
        {{ formError }}
      </p>
    </form>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton variant="ghost" :disabled="saving" @click="closeForm">
          {{ t('cancel') }}
        </ConsoleButton>
        <ConsoleButton
          type="submit"
          form="prefill-group-form"
          :loading="saving"
        >
          {{ saving ? t('saving') : form.id ? t('saveChanges') : t('create') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
  <ConfirmDialog
    :open="deleting !== null"
    :title="t('deleteGroup')"
    :message="t('deleteGroupMessage', { name: deleting?.name ?? '' })"
    :confirm-text="t('remove')"
    :loading="deleteBusy"
    @confirm="confirmDelete"
    @cancel="cancelDelete"
  >
    <p
      v-if="deleteError"
      role="alert"
      class="mt-3 text-sm text-[var(--status-danger-text)]"
    >
      {{ deleteError }}
    </p>
  </ConfirmDialog>
</template>
