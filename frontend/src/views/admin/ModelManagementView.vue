<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  ToggleLeft,
  ToggleRight,
  ArrowDownToLine,
} from 'lucide-vue-next'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import TextInput from '@/components/common/TextInput.vue'
import StringListEditor from '@/components/admin/StringListEditor.vue'
import MetadataSyncDialog from '@/components/admin/MetadataSyncDialog.vue'
import {
  metadataApi,
  type ModelMetadata,
  type ModelVendor,
  type PrefillGroup,
  type ModelInput,
  type VendorInput,
  type PrefillInput,
} from '@/api/adminManagement'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { adminManagementMessages } from '@/i18n/adminManagement'
import { useI18n } from 'vue-i18n'
import '@/components/admin/admin.css'

const props = defineProps<{ section: 'models' | 'vendors' | 'groups' }>()
const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
const request = useAdminRequest()
const page = ref(1)
const pageSize = ref(20)
const search = ref('')
const modelRows = ref<ModelMetadata[]>([])
const vendorRows = ref<ModelVendor[]>([])
const groupRows = ref<PrefillGroup[]>([])
const total = ref(0)
const modal = ref(false)
const deleting = ref<{ id: number; name: string } | null>(null)
const syncOpen = ref(false)
const modelForm = reactive<ModelInput>({
  model_name: '',
  description: '',
  icon: '',
  tags: '',
  vendor_id: 0,
  endpoints: '',
  status: 1,
  sync_official: 1,
  name_rule: 0,
})
const vendorForm = reactive<VendorInput>({
  name: '',
  description: '',
  icon: '',
  status: 1,
})
const groupForm = reactive<PrefillInput>({
  name: '',
  type: 'model',
  items: [],
  description: '',
})
const editingId = ref<number | undefined>()

const title = computed(() =>
  props.section === 'models'
    ? t('models')
    : props.section === 'vendors'
      ? t('vendors')
      : t('groups')
)
async function load() {
  await request.run(async (signal) => {
    if (props.section === 'models') {
      const result = await metadataApi.models(
        {
          p: page.value,
          page_size: pageSize.value,
          keyword: search.value || undefined,
        },
        signal
      )
      modelRows.value = result.items
      total.value = result.total
    } else if (props.section === 'vendors') {
      const result = await metadataApi.vendors(
        {
          p: page.value,
          page_size: pageSize.value,
          keyword: search.value || undefined,
        },
        signal
      )
      vendorRows.value = result.items
      total.value = result.total
    } else {
      groupRows.value = await metadataApi.groups(signal)
      total.value = groupRows.value.length
    }
  })
}
watch([() => props.section, page, pageSize], load)
onMounted(load)
function resetForm() {
  editingId.value = undefined
  Object.assign(modelForm, {
    model_name: '',
    description: '',
    icon: '',
    tags: '',
    vendor_id: 0,
    endpoints: '',
    status: 1,
    sync_official: 1,
    name_rule: 0,
  })
  Object.assign(vendorForm, { name: '', description: '', icon: '', status: 1 })
  Object.assign(groupForm, {
    name: '',
    type: 'model',
    items: [],
    description: '',
  })
}
function edit(row: ModelMetadata | ModelVendor | PrefillGroup) {
  resetForm()
  editingId.value = row.id
  if (props.section === 'models') Object.assign(modelForm, row as ModelMetadata)
  else if (props.section === 'vendors')
    Object.assign(vendorForm, row as ModelVendor)
  else Object.assign(groupForm, row as PrefillGroup)
  modal.value = true
}
function openCreate() {
  resetForm()
  modal.value = true
}
async function save() {
  if (
    (props.section === 'models' && !modelForm.model_name.trim()) ||
    (props.section === 'vendors' && !vendorForm.name.trim()) ||
    (props.section === 'groups' && !groupForm.name.trim())
  )
    return
  await request.run(async () => {
    if (props.section === 'models')
      await metadataApi.saveModel({ ...modelForm, id: editingId.value })
    else if (props.section === 'vendors')
      await metadataApi.saveVendor({ ...vendorForm, id: editingId.value })
    else await metadataApi.saveGroup({ ...groupForm, id: editingId.value })
    modal.value = false
    await load()
  })
}
async function remove() {
  if (!deleting.value) return
  const item = deleting.value
  await request.run(async () => {
    if (props.section === 'models') await metadataApi.deleteModel(item.id)
    else if (props.section === 'vendors')
      await metadataApi.deleteVendor(item.id)
    else await metadataApi.deleteGroup(item.id)
    deleting.value = null
    await load()
  })
}
async function toggle(row: ModelMetadata) {
  await request.run(async () => {
    await metadataApi.setModelStatus(row.id, row.status === 1 ? 0 : 1)
    await load()
  })
}
function handleSyncSaved() {
  syncOpen.value = false
  void load()
}
</script>
<template>
  <div class="admin-page">
    <div class="admin-toolbar">
      <h1>{{ title }}</h1>
      <TextInput
        v-model="search"
        :placeholder="t('search')"
        class="max-w-xs"
        @keyup.enter="load"
      /><ConsoleButton
        variant="ghost"
        :loading="request.loading.value"
        @click="load"
        ><RefreshCw :size="16" />{{ t('refresh') }}</ConsoleButton
      ><ConsoleButton variant="primary" @click="openCreate"
        ><Plus :size="16" />{{ t('create') }}</ConsoleButton
      ><ConsoleButton
        v-if="props.section === 'models'"
        variant="secondary"
        @click="syncOpen = true"
        ><ArrowDownToLine :size="16" />{{ t('synchronize') }}</ConsoleButton
      >
    </div>
    <p v-if="request.error" class="admin-error" role="alert">
      {{ request.error }}
    </p>
    <ConsoleCard :padded="false"
      ><div v-if="props.section === 'models'" class="admin-table-scroll">
        <table class="admin-table">
          <thead>
            <tr>
              <th>{{ t('modelName') }}</th>
              <th>{{ t('vendor') }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('endpoints') }}</th>
              <th>{{ t('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in modelRows" :key="row.id">
              <td>
                <strong>{{ row.model_name }}</strong
                ><br /><span class="admin-muted">{{ row.description }}</span>
              </td>
              <td>{{ row.vendor_id || t('noVendor') }}</td>
              <td>{{ row.status ? t('enabled') : t('disabled') }}</td>
              <td>{{ row.endpoints || '—' }}</td>
              <td>
                <div class="admin-row-actions">
                  <IconButton :label="t('edit')" @click="edit(row)"
                    ><Pencil :size="15" /></IconButton
                  ><IconButton
                    :label="row.status ? t('disabled') : t('enabled')"
                    @click="toggle(row)"
                    ><ToggleRight v-if="row.status" :size="15" /><ToggleLeft
                      v-else
                      :size="15" /></IconButton
                  ><IconButton
                    :label="t('remove')"
                    tone="danger"
                    @click="deleting = { id: row.id, name: row.model_name }"
                    ><Trash2 :size="15"
                  /></IconButton>
                </div>
              </td>
            </tr>
            <tr v-if="!modelRows.length">
              <td colspan="5" class="admin-muted">{{ t('empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else-if="props.section === 'vendors'" class="admin-table-scroll">
        <table class="admin-table">
          <thead>
            <tr>
              <th>{{ t('name') }}</th>
              <th>{{ t('description') }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in vendorRows" :key="row.id">
              <td>{{ row.name }}</td>
              <td>{{ row.description || '—' }}</td>
              <td>{{ row.status ? t('enabled') : t('disabled') }}</td>
              <td>
                <div class="admin-row-actions">
                  <IconButton :label="t('edit')" @click="edit(row)"
                    ><Pencil :size="15" /></IconButton
                  ><IconButton
                    :label="t('remove')"
                    tone="danger"
                    @click="deleting = { id: row.id, name: row.name }"
                    ><Trash2 :size="15"
                  /></IconButton>
                </div>
              </td>
            </tr>
            <tr v-if="!vendorRows.length">
              <td colspan="4" class="admin-muted">{{ t('empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="admin-table-scroll">
        <table class="admin-table">
          <thead>
            <tr>
              <th>{{ t('name') }}</th>
              <th>{{ t('type') }}</th>
              <th>{{ t('items') }}</th>
              <th>{{ t('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in groupRows" :key="row.id">
              <td>{{ row.name }}</td>
              <td>{{ t(row.type) }}</td>
              <td>{{ row.items.join(', ') }}</td>
              <td>
                <div class="admin-row-actions">
                  <IconButton :label="t('edit')" @click="edit(row)"
                    ><Pencil :size="15" /></IconButton
                  ><IconButton
                    :label="t('remove')"
                    tone="danger"
                    @click="deleting = { id: row.id, name: row.name }"
                    ><Trash2 :size="15"
                  /></IconButton>
                </div>
              </td>
            </tr>
            <tr v-if="!groupRows.length">
              <td colspan="4" class="admin-muted">{{ t('empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <TablePagination
        v-if="props.section !== 'groups'"
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
    /></ConsoleCard>
    <ConsoleModal
      :open="modal"
      :title="`${editingId ? t('edit') : t('create')} · ${title}`"
      size="lg"
      @close="modal = false"
      ><form class="admin-form" @submit.prevent="save">
        <div v-if="props.section === 'models'" class="admin-fields">
          <label class="admin-field"
            ><span>{{ t('modelName') }}</span
            ><input v-model="modelForm.model_name" required /></label
          ><label class="admin-field"
            ><span>{{ t('vendor') }}</span
            ><input
              v-model.number="modelForm.vendor_id"
              type="number"
              min="0" /></label
          ><label class="admin-field admin-field-wide"
            ><span>{{ t('description') }}</span
            ><textarea v-model="modelForm.description" /></label
          ><label class="admin-field"
            ><span>{{ t('tags') }}</span
            ><input v-model="modelForm.tags" /></label
          ><label class="admin-field"
            ><span>{{ t('endpoints') }}</span
            ><input
              v-model="modelForm.endpoints"
              :placeholder="t('invalidEndpoint')" /></label
          ><label class="admin-field"
            ><span>{{ t('rule') }}</span
            ><select v-model.number="modelForm.name_rule">
              <option :value="0">{{ t('exact') }}</option>
              <option :value="1">{{ t('prefix') }}</option>
              <option :value="2">{{ t('contains') }}</option>
              <option :value="3">{{ t('suffix') }}</option>
            </select></label
          ><label class="admin-check"
            ><input
              v-model="modelForm.status"
              type="checkbox"
              :true-value="1"
              :false-value="0"
            />{{ t('enabled') }}</label
          ><label class="admin-check"
            ><input
              v-model="modelForm.sync_official"
              type="checkbox"
              :true-value="1"
              :false-value="0"
            />{{ t('syncOfficial') }}</label
          >
        </div>
        <div v-else-if="props.section === 'vendors'" class="admin-fields">
          <label class="admin-field"
            ><span>{{ t('name') }}</span
            ><input v-model="vendorForm.name" required /></label
          ><label class="admin-field admin-field-wide"
            ><span>{{ t('description') }}</span
            ><textarea v-model="vendorForm.description" /></label
          ><label class="admin-check"
            ><input
              v-model="vendorForm.status"
              type="checkbox"
              :true-value="1"
              :false-value="0"
            />{{ t('enabled') }}</label
          >
        </div>
        <div v-else class="admin-fields">
          <label class="admin-field"
            ><span>{{ t('name') }}</span
            ><input v-model="groupForm.name" required /></label
          ><label class="admin-field"
            ><span>{{ t('type') }}</span
            ><select v-model="groupForm.type">
              <option value="model">{{ t('model') }}</option>
              <option value="tag">{{ t('tag') }}</option>
              <option value="endpoint">{{ t('endpoint') }}</option>
            </select></label
          ><label class="admin-field admin-field-wide"
            ><span>{{ t('description') }}</span
            ><textarea v-model="groupForm.description" />
          </label>
          <div class="admin-field admin-field-wide">
            <StringListEditor v-model="groupForm.items" :label="t('items')" />
          </div>
        </div>
        <div class="admin-row-actions">
          <ConsoleButton type="submit" :loading="request.loading.value"
            ><span>{{ t('save') }}</span></ConsoleButton
          ><ConsoleButton
            variant="ghost"
            type="button"
            @click="modal = false"
            >{{ t('cancel') }}</ConsoleButton
          >
        </div>
      </form></ConsoleModal
    >
    <ConfirmDialog
      :open="Boolean(deleting)"
      :title="t('confirmDelete')"
      :message="t('deleteMessage', { name: deleting?.name })"
      :confirm-text="t('remove')"
      :loading="request.loading.value"
      @confirm="remove"
      @cancel="deleting = null"
    />
    <MetadataSyncDialog
      :open="syncOpen"
      @close="syncOpen = false"
      @saved="handleSyncSaved"
    />
  </div>
</template>
