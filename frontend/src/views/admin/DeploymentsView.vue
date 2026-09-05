<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Eye, FileText, Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import IconButton from '@/components/common/IconButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import {
  deploymentsApi,
  type Deployment,
  type DeploymentConfig,
  type DeploymentCreateInput,
  type DeploymentDetail,
  type Hardware,
  type DeploymentLocation,
} from '@/api/adminManagement'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { adminManagementMessages } from '@/i18n/adminManagement'
import { useI18n } from 'vue-i18n'
import '@/components/admin/admin.css'

const { t } = useI18n({ useScope: 'local', messages: adminManagementMessages })
const request = useAdminRequest()
const rows = ref<Deployment[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const search = ref('')
const detail = ref<DeploymentDetail | null>(null)
const containers = ref<Awaited<ReturnType<typeof deploymentsApi.containers>>>(
  []
)
const logs = ref('')
const deleting = ref<Deployment | null>(null)
const modal = ref<'create' | 'edit' | 'rename' | 'extend' | null>(null)
const settings = ref<{
  enabled: boolean
  configured: boolean
  can_connect: boolean
} | null>(null)
const hardware = ref<Hardware[]>([])
const locations = ref<DeploymentLocation[]>([])
const quote = ref<{
  estimated_cost: number
  currency: string
  estimation_valid: boolean
  hourly_rate: number
} | null>(null)
const selectedId = ref('')
const nameForm = ref('')
const extendHours = ref(1)
const form = reactive<DeploymentCreateInput>({
  resource_private_name: '',
  duration_hours: 1,
  gpus_per_container: 1,
  hardware_id: 0,
  location_ids: [],
  container_config: {
    replica_count: 1,
    env_variables: {},
    entrypoint: [],
    traffic_port: 0,
    args: [],
  },
  registry_config: { image_url: '' },
})
const editForm = reactive<
  Partial<DeploymentConfig> & {
    args?: string[]
    registry_username?: string
    registry_secret?: string
    secret_env_variables?: Record<string, string>
  }
>({
  image_url: '',
  traffic_port: 0,
  entrypoint: [],
  env_variables: {},
  args: [],
})
const enabled = computed(() =>
  Boolean(settings.value?.enabled && settings.value?.configured)
)
async function load() {
  await request.run(async (signal) => {
    settings.value ??= await deploymentsApi.settings(signal)
    const pageData = await deploymentsApi.list(
      {
        p: page.value,
        page_size: pageSize.value,
        keyword: search.value || undefined,
      },
      signal
    )
    rows.value = pageData.items
    total.value = pageData.total
    if (enabled.value && !hardware.value.length)
      [hardware.value, locations.value] = await Promise.all([
        deploymentsApi.hardware(signal),
        deploymentsApi.locations(signal),
      ])
  })
}
onMounted(load)
watch([page, pageSize], load)
function resetCreate() {
  Object.assign(form, {
    resource_private_name: '',
    duration_hours: 1,
    gpus_per_container: 1,
    hardware_id: hardware.value[0]?.id ?? 0,
    location_ids: [],
    container_config: {
      replica_count: 1,
      env_variables: {},
      entrypoint: [],
      traffic_port: 0,
      args: [],
    },
    registry_config: { image_url: '' },
  })
  quote.value = null
}
function openCreate() {
  resetCreate()
  modal.value = 'create'
}
function openRename() {
  if (!detail.value) return
  nameForm.value = detail.value.deployment_name
  modal.value = 'rename'
}
async function loadDetail(id: string) {
  selectedId.value = id
  await request.run(async (signal) => {
    detail.value = await deploymentsApi.detail(id, signal)
    containers.value = await deploymentsApi.containers(id, signal)
    editForm.image_url = detail.value.container_config.image_url
    editForm.traffic_port = detail.value.container_config.traffic_port
    editForm.entrypoint = detail.value.container_config.entrypoint
    editForm.env_variables = { ...detail.value.container_config.env_variables }
  })
}
async function estimate() {
  if (!form.hardware_id || !form.location_ids.length) return
  await request.run(async (signal) => {
    quote.value = await deploymentsApi.quote(
      {
        hardware_id: form.hardware_id,
        location_ids: form.location_ids,
        gpus_per_container: form.gpus_per_container,
        duration_hours: form.duration_hours,
        replica_count: form.container_config.replica_count,
        currency: 'usdc',
      },
      signal
    )
  })
}
async function create() {
  if (
    !form.resource_private_name ||
    !form.registry_config.image_url ||
    !quote.value?.estimation_valid
  )
    return
  await request.run(async () => {
    await deploymentsApi.create(form)
    modal.value = null
    await load()
  })
}
async function saveEdit() {
  if (!selectedId.value) return
  await request.run(async () => {
    await deploymentsApi.update(selectedId.value, editForm)
    modal.value = null
    await loadDetail(selectedId.value)
  })
}
async function rename() {
  if (!selectedId.value || !nameForm.value.trim()) return
  await request.run(async () => {
    await deploymentsApi.rename(selectedId.value, nameForm.value.trim())
    modal.value = null
    await load()
  })
}
async function extend() {
  if (!selectedId.value || extendHours.value < 1) return
  await request.run(async () => {
    await deploymentsApi.extend(selectedId.value, extendHours.value)
    modal.value = null
    await loadDetail(selectedId.value)
  })
}
async function remove() {
  if (!deleting.value) return
  await request.run(async () => {
    await deploymentsApi.delete(deleting.value!.id)
    deleting.value = null
    detail.value = null
    await load()
  })
}
async function fetchLogs() {
  if (!selectedId.value || !containers.value[0]) return
  await request.run(async (signal) => {
    logs.value = await deploymentsApi.logs(
      selectedId.value,
      {
        container_id: containers.value[0].container_id,
        stream: 'all',
        limit: 200,
      },
      signal
    )
  })
}
</script>
<template>
  <div class="admin-page">
    <div class="admin-toolbar">
      <h1>{{ t('deployments') }}</h1>
      <ConsoleButton
        variant="ghost"
        :loading="request.loading.value"
        @click="load"
        ><RefreshCw :size="16" />{{ t('refresh') }}</ConsoleButton
      ><ConsoleButton variant="primary" :disabled="!enabled" @click="openCreate"
        ><Plus :size="16" />{{ t('create') }}</ConsoleButton
      >
    </div>
    <p v-if="!enabled && settings" class="admin-error">
      {{ t('deploymentDisabled') }}
    </p>
    <p v-if="request.error" class="admin-error" role="alert">
      {{ request.error }}
    </p>
    <ConsoleCard :padded="false"
      ><div class="admin-table-scroll">
        <table class="admin-table">
          <thead>
            <tr>
              <th>{{ t('name') }}</th>
              <th>{{ t('status') }}</th>
              <th>{{ t('hardware') }}</th>
              <th>{{ t('remaining') }}</th>
              <th>{{ t('progress') }}</th>
              <th>{{ t('actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id">
              <td>{{ row.deployment_name }}</td>
              <td>{{ row.status }}</td>
              <td>{{ row.hardware_name }} × {{ row.hardware_quantity }}</td>
              <td>{{ row.compute_minutes_remaining }}</td>
              <td>{{ row.completed_percent.toFixed(1) }}%</td>
              <td>
                <div class="admin-row-actions">
                  <IconButton :label="t('details')" @click="loadDetail(row.id)"
                    ><Eye :size="15" /></IconButton
                  ><IconButton
                    :label="t('remove')"
                    tone="danger"
                    @click="deleting = row"
                    ><Trash2 :size="15"
                  /></IconButton>
                </div>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="6" class="admin-muted">{{ t('empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <TablePagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
    /></ConsoleCard>
    <ConsoleModal
      :open="detail !== null"
      :title="detail?.deployment_name ?? t('details')"
      size="xl"
      @close="detail = null"
      ><div v-if="detail" class="admin-form">
        <dl class="admin-values">
          <div>
            <dt>{{ t('status') }}</dt>
            <dd>{{ detail.status }}</dd>
          </div>
          <div>
            <dt>{{ t('paid') }}</dt>
            <dd>{{ detail.amount_paid }}</dd>
          </div>
          <div>
            <dt>{{ t('hardware') }}</dt>
            <dd>
              {{ detail.brand_name }} {{ detail.hardware_name }} ×
              {{ detail.total_gpus }}
            </dd>
          </div>
          <div>
            <dt>{{ t('containers') }}</dt>
            <dd>{{ detail.total_containers }}</dd>
          </div>
          <div>
            <dt>{{ t('locations') }}</dt>
            <dd>{{ detail.locations.map((item) => item.name).join(', ') }}</dd>
          </div>
        </dl>
        <div class="admin-row-actions">
          <ConsoleButton size="sm" variant="secondary" @click="openRename"
            ><Pencil :size="15" />{{ t('rename') }}</ConsoleButton
          ><ConsoleButton
            size="sm"
            variant="secondary"
            @click="modal = 'extend'"
            >{{ t('extend') }}</ConsoleButton
          ><ConsoleButton size="sm" variant="ghost" @click="modal = 'edit'"
            ><Pencil :size="15" />{{ t('edit') }}</ConsoleButton
          ><ConsoleButton size="sm" variant="ghost" @click="fetchLogs"
            ><FileText :size="15" />{{ t('logs') }}</ConsoleButton
          >
        </div>
        <div class="admin-section">
          <h2>{{ t('containers') }}</h2>
          <table class="admin-table">
            <thead>
              <tr>
                <th>{{ t('name') }}</th>
                <th>{{ t('status') }}</th>
                <th>{{ t('device') }}</th>
                <th>{{ t('uptime') }}</th>
                <th>{{ t('publicUrl') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="container in containers" :key="container.container_id">
                <td>{{ container.container_id }}</td>
                <td>{{ container.status }}</td>
                <td>{{ container.device_id }}</td>
                <td>{{ container.uptime_percent }}%</td>
                <td>
                  <a
                    :href="container.public_url"
                    target="_blank"
                    rel="noreferrer"
                    >{{ container.public_url || '—' }}</a
                  >
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <pre v-if="logs" class="admin-log">{{ logs }}</pre>
      </div></ConsoleModal
    >
    <ConsoleModal
      :open="modal === 'create'"
      :title="t('create')"
      size="lg"
      @close="modal = null"
      ><form class="admin-form" @submit.prevent="create">
        <div class="admin-fields">
          <label class="admin-field"
            ><span>{{ t('name') }}</span
            ><input v-model="form.resource_private_name" required /></label
          ><label class="admin-field"
            ><span>{{ t('image') }}</span
            ><input v-model="form.registry_config.image_url" required /></label
          ><label class="admin-field"
            ><span>{{ t('hardware') }}</span
            ><select v-model.number="form.hardware_id" required>
              <option v-for="item in hardware" :key="item.id" :value="item.id">
                {{ item.name }} · {{ item.hourly_rate }}/h
              </option>
            </select></label
          ><label class="admin-field"
            ><span>{{ t('gpuCount') }}</span
            ><input
              v-model.number="form.gpus_per_container"
              min="1"
              type="number"
              required /></label
          ><label class="admin-field"
            ><span>{{ t('replicas') }}</span
            ><input
              v-model.number="form.container_config.replica_count"
              min="1"
              type="number"
              required /></label
          ><label class="admin-field"
            ><span>{{ t('duration') }}</span
            ><input
              v-model.number="form.duration_hours"
              min="1"
              type="number"
              required /></label
          ><label class="admin-field admin-field-wide"
            ><span>{{ t('locations') }}</span
            ><select v-model="form.location_ids" multiple required>
              <option v-for="item in locations" :key="item.id" :value="item.id">
                {{ item.name }}
              </option>
            </select></label
          ><label class="admin-field"
            ><span>{{ t('port') }}</span
            ><input
              v-model.number="form.container_config.traffic_port"
              type="number"
              min="1"
              max="65535"
          /></label>
        </div>
        <div class="admin-row-actions">
          <ConsoleButton type="button" variant="secondary" @click="estimate">{{
            t('quote')
          }}</ConsoleButton
          ><span v-if="quote" class="admin-muted"
            >{{ t('totalCost') }}: {{ quote.estimated_cost }}
            {{ quote.currency }} · {{ quote.hourly_rate }}/h</span
          >
        </div>
        <div class="admin-row-actions">
          <ConsoleButton
            type="submit"
            :disabled="!quote?.estimation_valid"
            :loading="request.loading.value"
            ><Plus :size="16" />{{ t('create') }}</ConsoleButton
          ><ConsoleButton type="button" variant="ghost" @click="modal = null">{{
            t('cancel')
          }}</ConsoleButton>
        </div>
      </form></ConsoleModal
    >
    <ConsoleModal
      :open="modal === 'edit'"
      :title="t('edit')"
      size="lg"
      @close="modal = null"
      ><form class="admin-form" @submit.prevent="saveEdit">
        <label class="admin-field"
          ><span>{{ t('image') }}</span
          ><input v-model="editForm.image_url" required /></label
        ><label class="admin-field"
          ><span>{{ t('port') }}</span
          ><input
            v-model.number="editForm.traffic_port"
            type="number"
            min="1"
            max="65535" /></label
        ><label class="admin-field"
          ><span>{{ t('entrypoint') }}</span
          ><input
            :value="editForm.entrypoint?.join(' ')"
            @input="
              editForm.entrypoint = ($event.target as HTMLInputElement).value
                .split(/\s+/)
                .filter(Boolean)
            "
        /></label>
        <div class="admin-row-actions">
          <ConsoleButton type="submit" :loading="request.loading.value">{{
            t('save')
          }}</ConsoleButton
          ><ConsoleButton type="button" variant="ghost" @click="modal = null">{{
            t('cancel')
          }}</ConsoleButton>
        </div>
      </form></ConsoleModal
    >
    <ConsoleModal
      :open="modal === 'rename'"
      :title="t('rename')"
      size="sm"
      @close="modal = null"
      ><form class="admin-form" @submit.prevent="rename">
        <label class="admin-field"
          ><span>{{ t('name') }}</span
          ><input v-model="nameForm" required /></label
        ><ConsoleButton type="submit">{{ t('save') }}</ConsoleButton>
      </form></ConsoleModal
    ><ConsoleModal
      :open="modal === 'extend'"
      :title="t('extend')"
      size="sm"
      @close="modal = null"
      ><form class="admin-form" @submit.prevent="extend">
        <label class="admin-field"
          ><span>{{ t('duration') }}</span
          ><input
            v-model.number="extendHours"
            type="number"
            min="1"
            required /></label
        ><ConsoleButton type="submit">{{ t('confirmExtend') }}</ConsoleButton>
      </form></ConsoleModal
    >
    <ConfirmDialog
      :open="deleting !== null"
      :title="t('confirmDelete')"
      :message="t('deleteMessage', { name: deleting?.deployment_name })"
      :confirm-text="t('remove')"
      :loading="request.loading.value"
      @confirm="remove"
      @cancel="deleting = null"
    />
  </div>
</template>
