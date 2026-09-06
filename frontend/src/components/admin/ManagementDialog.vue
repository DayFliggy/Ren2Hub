<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-vue-next'
import {
  metadataApi,
  type ModelVendor,
  type PrefillGroup,
  type VendorInput,
} from '@/api/adminManagement'
import { adminManagementMessages } from '@/i18n/adminManagement'
import {
  MODEL_ENDPOINT_TEMPLATES,
  parseModelEndpoints,
} from '@/utils/modelEndpoints'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import StringListEditor from './StringListEditor.vue'
import MetadataIcon from './MetadataIcon.vue'

const props = defineProps<{
  open: boolean
  kind: 'vendors' | 'prefill-groups' | null
}>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
const vendors = ref<ModelVendor[]>([])
const groups = ref<PrefillGroup[]>([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const search = ref('')
const filter = ref<'' | PrefillGroup['type']>('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const formError = ref('')
const notice = ref('')
const editing = ref(false)
const deleting = ref<{ id: number; name: string } | null>(null)
let request: AbortController | null = null
const vendor = reactive<VendorInput>({
  name: '',
  description: '',
  icon: '',
  status: 1,
})
const group = reactive({
  id: undefined as number | undefined,
  name: '',
  description: '',
  type: 'model' as PrefillGroup['type'],
  list: [] as string[],
  json: '{}',
})
const title = computed(() => t(props.kind === 'vendors' ? 'vendors' : 'groups'))
const filteredGroups = computed(() =>
  groups.value.filter(
    (item) =>
      (!filter.value || item.type === filter.value) &&
      `${item.name} ${item.description}`
        .toLowerCase()
        .includes(search.value.trim().toLowerCase())
  )
)
const pageGroups = computed(() =>
  filteredGroups.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)

async function load() {
  if (!props.open) return
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  error.value = ''
  try {
    if (props.kind === 'vendors') {
      const data = await metadataApi.vendors(
        {
          p: page.value,
          page_size: pageSize.value,
          keyword: search.value.trim(),
        },
        controller.signal
      )
      if (controller.signal.aborted) return
      vendors.value = data.items
      total.value = data.total
    } else {
      const data = await metadataApi.groups(controller.signal)
      if (controller.signal.aborted) return
      groups.value = data
    }
  } catch (cause) {
    if (!controller.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (request === controller) loading.value = false
  }
}
watch(
  [() => props.open, () => props.kind],
  () => {
    request?.abort()
    editing.value = false
    deleting.value = null
    page.value = 1
    search.value = ''
    filter.value = ''
    notice.value = ''
    if (props.open) void load()
  },
  { immediate: true }
)
watch([page, pageSize], () => {
  if (props.kind === 'vendors') void load()
})
watch([search, filter], () => {
  if (props.kind === 'prefill-groups') page.value = 1
})
onBeforeUnmount(() => request?.abort())
function searchVendors() {
  if (page.value !== 1) page.value = 1
  else void load()
}
function editVendor(row?: ModelVendor) {
  Object.assign(
    vendor,
    row ?? { id: undefined, name: '', description: '', icon: '', status: 1 }
  )
  formError.value = ''
  editing.value = true
}
function editGroup(row?: PrefillGroup) {
  Object.assign(group, {
    id: row?.id,
    name: row?.name ?? '',
    description: row?.description ?? '',
    type: row?.type ?? (filter.value || 'model'),
    list: row && row.type !== 'endpoint' ? [...row.items] : [],
    json: row?.type === 'endpoint' ? row.items : '{}',
  })
  formError.value = ''
  editing.value = true
}
async function save() {
  if (saving.value) return
  formError.value = ''
  saving.value = true
  try {
    if (props.kind === 'vendors') {
      if (!vendor.name.trim()) throw new Error(t('required'))
      await metadataApi.saveVendor({ ...vendor, name: vendor.name.trim() })
    } else {
      if (!group.name.trim()) throw new Error(t('required'))
      const base = {
        id: group.id,
        name: group.name.trim(),
        description: group.description,
      }
      if (group.type === 'endpoint') {
        let endpoints
        try {
          endpoints = parseModelEndpoints(group.json)
        } catch {
          throw new Error(t('invalidEndpoint'))
        }
        await metadataApi.saveGroup({
          ...base,
          type: 'endpoint',
          items: JSON.stringify(endpoints),
        })
      } else {
        const items = group.list.map((item) => item.trim())
        if (items.some((item) => !item) || new Set(items).size !== items.length)
          throw new Error(t('invalidList'))
        await metadataApi.saveGroup({ ...base, type: group.type, items })
      }
    }
    editing.value = false
    notice.value = t('saved')
    emit('saved')
    await load()
  } catch (cause) {
    formError.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}
async function remove() {
  if (!deleting.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    if (props.kind === 'vendors')
      await metadataApi.deleteVendor(deleting.value.id)
    else await metadataApi.deleteGroup(deleting.value.id)
    deleting.value = null
    notice.value = t('deleted')
    emit('saved')
    await load()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
    deleting.value = null
  } finally {
    saving.value = false
  }
}
</script>
<template>
  <ConsoleModal
    :open="open"
    :title="title"
    size="xl"
    :close-disabled="saving"
    @close="emit('close')"
  >
    <div class="admin-form">
      <form class="admin-toolbar" @submit.prevent="searchVendors">
        <label class="admin-field flex-1"
          ><span>{{ t('search') }}</span
          ><input v-model="search" type="search"
        /></label>
        <label v-if="kind === 'prefill-groups'" class="admin-field"
          ><span>{{ t('type') }}</span
          ><select v-model="filter">
            <option value="">{{ t('all') }}</option>
            <option
              v-for="type in ['model', 'tag', 'endpoint']"
              :key="type"
              :value="type"
            >
              {{ t(type) }}
            </option>
          </select></label
        >
        <ConsoleButton
          v-if="kind === 'vendors'"
          type="submit"
          variant="secondary"
          >{{ t('search') }}</ConsoleButton
        >
        <IconButton :label="t('refresh')" :disabled="loading" @click="load"
          ><RefreshCw :size="16"
        /></IconButton>
        <ConsoleButton @click="kind === 'vendors' ? editVendor() : editGroup()"
          ><Plus :size="16" />{{ t('create') }}</ConsoleButton
        >
      </form>
      <p v-if="error" role="alert" class="admin-error">{{ error }}</p>
      <p v-if="notice" role="status" class="admin-muted">{{ notice }}</p>
      <p v-if="loading" role="status" class="admin-muted">{{ t('loading') }}</p>
      <div v-else class="management-list">
        <template v-if="kind === 'vendors'">
          <article v-for="row in vendors" :key="row.id" class="management-row">
            <MetadataIcon :name="row.name" :icon="row.icon" />
            <div class="min-w-0 flex-1">
              <strong>{{ row.name }}</strong>
              <p class="admin-muted">{{ row.description }}</p>
              <span class="admin-muted">{{
                row.status ? t('enabled') : t('disabled')
              }}</span>
            </div>
            <div class="admin-row-actions">
              <IconButton :label="t('edit')" @click="editVendor(row)"
                ><Pencil :size="16" /></IconButton
              ><IconButton
                :label="t('remove')"
                tone="danger"
                @click="deleting = { id: row.id, name: row.name }"
                ><Trash2 :size="16"
              /></IconButton>
            </div>
          </article>
          <p v-if="!vendors.length" class="admin-muted">{{ t('empty') }}</p>
        </template>
        <template v-else>
          <article
            v-for="row in pageGroups"
            :key="row.id"
            class="management-row"
          >
            <div class="min-w-0 flex-1">
              <strong>{{ row.name }}</strong
              ><span class="admin-muted"> · {{ t(row.type) }}</span>
              <p class="admin-muted">{{ row.description }}</p>
              <p class="management-items">
                {{ row.type === 'endpoint' ? row.items : row.items.join(', ') }}
              </p>
            </div>
            <div class="admin-row-actions">
              <IconButton :label="t('edit')" @click="editGroup(row)"
                ><Pencil :size="16" /></IconButton
              ><IconButton
                :label="t('remove')"
                tone="danger"
                @click="deleting = { id: row.id, name: row.name }"
                ><Trash2 :size="16"
              /></IconButton>
            </div>
          </article>
          <p v-if="!pageGroups.length" class="admin-muted">{{ t('empty') }}</p>
        </template>
      </div>
      <TablePagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="kind === 'vendors' ? total : filteredGroups.length"
      />
    </div>
    <template #footer
      ><ConsoleButton variant="ghost" :disabled="saving" @click="emit('close')"
        ><X :size="16" />{{ t('close') }}</ConsoleButton
      ></template
    >
  </ConsoleModal>
  <ConsoleModal
    :open="editing"
    :title="`${t('edit')} · ${title}`"
    presentation="drawer"
    size="lg"
    :close-disabled="saving"
    @close="editing = false"
  >
    <form
      id="metadata-management-form"
      class="admin-form"
      @submit.prevent="save"
    >
      <fieldset class="admin-fields" :disabled="saving">
        <template v-if="kind === 'vendors'">
          <label class="admin-field"
            ><span>{{ t('name') }}</span
            ><input v-model="vendor.name" required maxlength="64"
          /></label>
          <label class="admin-field"
            ><span>{{ t('icon') }}</span
            ><input v-model="vendor.icon" maxlength="128"
          /></label>
          <MetadataIcon :icon="vendor.icon" :name="vendor.name" :size="40" />
          <label class="admin-check"
            ><input
              v-model="vendor.status"
              type="checkbox"
              :true-value="1"
              :false-value="0"
            />{{ t('enabled') }}</label
          >
          <label class="admin-field admin-field-wide"
            ><span>{{ t('description') }}</span
            ><textarea v-model="vendor.description" />
          </label>
        </template>
        <template v-else>
          <label class="admin-field"
            ><span>{{ t('name') }}</span
            ><input v-model="group.name" required maxlength="64"
          /></label>
          <label class="admin-field"
            ><span>{{ t('type') }}</span
            ><select v-model="group.type">
              <option
                v-for="type in ['model', 'tag', 'endpoint']"
                :key="type"
                :value="type"
              >
                {{ t(type) }}
              </option>
            </select></label
          >
          <label class="admin-field admin-field-wide"
            ><span>{{ t('description') }}</span
            ><textarea v-model="group.description" maxlength="255" />
          </label>
          <div
            v-if="group.type !== 'endpoint'"
            class="admin-field admin-field-wide"
          >
            <span>{{ t('items') }}</span
            ><StringListEditor v-model="group.list" :label="t('items')" />
          </div>
          <div v-else class="admin-field admin-field-wide">
            <label for="prefill-endpoint-json"
              >{{ t('endpoints') }} (JSON)</label
            ><textarea
              id="prefill-endpoint-json"
              v-model="group.json"
              rows="12"
              spellcheck="false"
            /><ConsoleButton
              variant="ghost"
              @click="
                group.json = JSON.stringify(MODEL_ENDPOINT_TEMPLATES, null, 2)
              "
              >{{ t('endpointTemplates') }}</ConsoleButton
            >
          </div>
        </template>
      </fieldset>
      <p v-if="formError" role="alert" class="admin-error">{{ formError }}</p>
    </form>
    <template #footer
      ><div class="admin-row-actions">
        <ConsoleButton
          type="submit"
          form="metadata-management-form"
          :loading="saving"
          >{{ t('save') }}</ConsoleButton
        ><ConsoleButton
          variant="ghost"
          :disabled="saving"
          @click="editing = false"
          >{{ t('cancel') }}</ConsoleButton
        >
      </div></template
    >
  </ConsoleModal>
  <ConfirmDialog
    :open="Boolean(deleting)"
    :title="t('confirmDelete')"
    :message="t('deleteMessage', { name: deleting?.name })"
    :confirm-text="t('remove')"
    :loading="saving"
    @confirm="remove"
    @cancel="deleting = null"
  />
</template>
<style scoped>
.management-list {
  min-width: 0;
}
.management-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.875rem 0;
  border-bottom: 1px solid var(--outline-variant);
  overflow-wrap: anywhere;
}
.management-items {
  font-size: 0.75rem;
  color: var(--text-secondary);
  white-space: pre-wrap;
  max-height: 6rem;
  overflow: auto;
}
</style>
