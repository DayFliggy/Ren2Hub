<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  automaticPricingApi,
  type AutomaticPricingStatus,
} from '@/api/automaticPricing'
import { isActiveSystemTask, systemManagementApi } from '@/api/systemManagement'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useSystemTask } from '@/composables/useSystemManagement'
import { formatSystemTimestamp } from '@/utils/systemManagement'
import ConsoleButton from '@/components/common/ConsoleButton.vue'

const { t, te, locale } = useI18n()
const emit = defineEmits<{ updated: [] }>()
const status = ref<AutomaticPricingStatus | null>(null)
const enabled = ref(true)
const minutes = ref(30)
const loading = ref(false)
const taskLoading = ref(false)
const latestTaskError = ref('')
const saving = ref(false)
const syncing = ref(false)
const error = ref('')
const feedback = ref('')
const request = useLatestRequest()
const latestTaskRequest = useLatestRequest()
const { task, error: taskError, refresh: refreshTask, follow } = useSystemTask()
const active = computed(() => task.value && isActiveSystemTask(task.value))
const validInterval = computed(
  () =>
    Number.isInteger(minutes.value) &&
    minutes.value >= 5 &&
    minutes.value <= 1440
)
const dirty = computed(
  () =>
    status.value &&
    (enabled.value !== status.value.config.enabled ||
      minutes.value !== status.value.config.check_interval_minutes)
)
let disposed = false
let completedTask = ''
let refreshAfterSave = false
const result = computed(() => task.value?.pricing_result)
const resultCounters = [
  'checked',
  'changed',
  'published',
  'deactivated',
  'unconfigured_count',
] as const
const unexplainedModels = computed(() => {
  const explained = new Set(
    result.value?.unconfigured_details.map((detail) => detail.model)
  )
  return (
    result.value?.unconfigured.filter((model) => !explained.has(model)) ?? []
  )
})

async function refreshStatus() {
  if (saving.value) {
    refreshAfterSave = true
    return
  }
  loading.value = true
  error.value = ''
  const result = await request.run(automaticPricingApi.status)
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  const preserveDraft = dirty.value
  status.value = result.value
  if (!preserveDraft) {
    enabled.value = result.value.config.enabled
    minutes.value = result.value.config.check_interval_minutes
  }
}
async function refreshLatestTask() {
  taskLoading.value = true
  latestTaskError.value = ''
  const response = await latestTaskRequest.run((signal) =>
    systemManagementApi.tasks(
      { type: 'billing_pricing_sync', limit: 1 },
      signal
    )
  )
  if (response.stale) return
  taskLoading.value = false
  if (!response.ok) {
    latestTaskError.value =
      response.error instanceof Error
        ? response.error.message
        : String(response.error)
    return
  }
  const latest = response.value.items[0]
  if (
    latest &&
    (latest.task_id !== task.value?.task_id ||
      latest.updated_at > task.value.updated_at ||
      (isActiveSystemTask(task.value) && !isActiveSystemTask(latest)))
  )
    follow(latest)
}
async function refresh() {
  if (saving.value || syncing.value) return
  await Promise.all([refreshStatus(), refreshLatestTask()])
}
async function save() {
  if (saving.value || loading.value || !validInterval.value || !status.value)
    return
  saving.value = true
  error.value = ''
  feedback.value = ''
  try {
    const config = await automaticPricingApi.configure({
      enabled: enabled.value,
      check_interval_minutes: minutes.value,
    })
    if (disposed) return
    status.value.config = config
    enabled.value = config.enabled
    minutes.value = config.check_interval_minutes
    feedback.value = t('billing.automatic.saved')
  } catch (cause) {
    if (!disposed)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) {
      saving.value = false
      if (refreshAfterSave) {
        refreshAfterSave = false
        void refreshStatus()
      }
    }
  }
}
async function sync() {
  if (syncing.value || active.value) return
  syncing.value = true
  latestTaskRequest.cancel()
  taskLoading.value = false
  latestTaskError.value = ''
  error.value = ''
  feedback.value = ''
  try {
    const result = await automaticPricingApi.sync()
    if (!disposed) follow(result)
  } catch (cause) {
    if (!disposed)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) syncing.value = false
  }
}
const downloadingSnapshot = ref(0)
async function downloadSnapshot(id: number) {
  if (downloadingSnapshot.value) return
  downloadingSnapshot.value = id
  error.value = ''
  try {
    const blob = await automaticPricingApi.downloadSnapshot(id)
    if (disposed) return
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `pricing-snapshot-${id}.json`
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 0)
  } catch (cause) {
    if (!disposed)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!disposed) downloadingSnapshot.value = 0
  }
}
watch(task, (value) => {
  if (!value || isActiveSystemTask(value)) return
  const key = `${value.task_id}:${value.status}:${value.updated_at}`
  if (key === completedTask) return
  completedTask = key
  emit('updated')
  void refreshStatus()
})
onMounted(refresh)
onBeforeUnmount(() => {
  disposed = true
})
defineExpose({ refresh })
</script>

<template>
  <section
    class="automatic-pricing"
    data-automatic-pricing
    :aria-busy="loading || taskLoading || saving || syncing"
  >
    <div class="automatic-heading">
      <div>
        <h2>{{ t('billing.automatic.title') }}</h2>
        <p class="admin-muted">{{ t('billing.automatic.priority') }}</p>
      </div>
      <div class="automatic-actions">
        <ConsoleButton
          variant="ghost"
          size="sm"
          :loading="loading || taskLoading"
          :disabled="saving || syncing"
          @click="refresh"
          >{{ t('billing.refresh') }}</ConsoleButton
        >
        <ConsoleButton
          variant="secondary"
          size="sm"
          :loading="syncing || Boolean(active)"
          :disabled="saving"
          data-automatic-sync
          @click="sync"
          >{{ t('billing.automatic.checkNow') }}</ConsoleButton
        >
      </div>
    </div>
    <p class="admin-muted">{{ t('billing.automatic.behavior') }}</p>
    <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
    <form v-if="status" class="automatic-config" @submit.prevent="save">
      <label class="admin-check"
        ><input v-model="enabled" type="checkbox" :disabled="saving" />{{
          t('billing.automatic.enabled')
        }}</label
      >
      <label class="admin-field"
        ><span>{{ t('billing.automatic.interval') }}</span
        ><input
          v-model.number="minutes"
          type="number"
          min="5"
          max="1440"
          step="1"
          required
          :disabled="saving"
      /></label>
      <ConsoleButton
        type="submit"
        size="sm"
        :loading="saving"
        :disabled="!dirty || !validInterval || loading"
        data-automatic-save
        >{{ t('billing.automatic.save') }}</ConsoleButton
      >
    </form>
    <p v-if="status && !validInterval" class="admin-error" role="alert">
      {{ t('billing.automatic.invalidInterval') }}
    </p>
    <p v-if="feedback" class="admin-muted" role="status">{{ feedback }}</p>
    <div v-if="task" class="automatic-task" role="status">
      <span>{{
        t('billing.automatic.task', {
          id: task.task_id,
          status: t(`billing.automatic.status.${task.status}`),
        })
      }}</span>
      <p v-if="task.error" class="admin-error">{{ task.error }}</p>
      <template v-if="result">
        <dl class="automatic-result" data-automatic-result>
          <div v-for="counter in resultCounters" :key="counter">
            <dt>{{ t(`billing.automatic.results.${counter}`) }}</dt>
            <dd>{{ result[counter] }}</dd>
          </div>
        </dl>
        <ul v-if="result.errors.length" class="automatic-errors">
          <li
            v-for="message in result.errors"
            :key="message"
            class="admin-error"
          >
            {{ message }}
          </li>
        </ul>
        <details
          v-if="result.unconfigured_count"
          class="automatic-unconfigured"
        >
          <summary>
            {{
              t('billing.automatic.unconfiguredDetails', {
                count: result.unconfigured_count,
              })
            }}
          </summary>
          <p class="admin-muted">{{ t('billing.automatic.detailsLimit') }}</p>
          <ul>
            <li
              v-for="detail in result.unconfigured_details"
              :key="detail.model"
            >
              <span class="font-mono">{{ detail.model }}</span>
              <ul>
                <li
                  v-for="reason in detail.reasons"
                  :key="`${reason.source}:${reason.reason}`"
                >
                  {{
                    te(`billing.automatic.origins.${reason.source}`)
                      ? t(`billing.automatic.origins.${reason.source}`)
                      : reason.source
                  }}: {{ reason.reason }}
                </li>
              </ul>
            </li>
            <li
              v-for="model in unexplainedModels"
              :key="model"
              class="font-mono"
            >
              {{ model }}
            </li>
          </ul>
        </details>
      </template>
    </div>
    <div v-if="latestTaskError" class="automatic-actions" role="alert">
      <span class="admin-error">{{ latestTaskError }}</span>
      <ConsoleButton variant="ghost" size="sm" @click="refreshLatestTask">{{
        t('billing.automatic.retryTask')
      }}</ConsoleButton>
    </div>
    <div v-if="taskError" class="automatic-actions" role="alert">
      <span class="admin-error">{{ taskError }}</span>
      <ConsoleButton variant="ghost" size="sm" @click="refreshTask">{{
        t('billing.automatic.retryTask')
      }}</ConsoleButton>
    </div>
    <div v-if="status" class="automatic-sources">
      <article
        v-for="source in status.sources"
        :key="source.source"
        class="automatic-source"
        :data-price-source="source.source"
      >
        <div class="automatic-source-title">
          <h3>{{ t(`billing.automatic.origins.${source.source}`) }}</h3>
          <span>{{ t(`billing.automatic.roles.${source.role}`) }}</span>
        </div>
        <ConsoleButton
          v-if="source.snapshot_id > 0"
          size="sm"
          variant="ghost"
          :disabled="Boolean(downloadingSnapshot)"
          :loading="downloadingSnapshot === source.snapshot_id"
          data-download-price-snapshot
          @click="downloadSnapshot(source.snapshot_id)"
          >{{ t('billing.automatic.downloadSnapshot') }}</ConsoleButton
        >
        <p class="automatic-url">{{ source.url }}</p>
        <dl>
          <div>
            <dt>{{ t('billing.automatic.snapshotEvidence') }}</dt>
            <dd>{{ source.snapshot_id }} · {{ source.parser_version }}</dd>
          </div>
          <div>
            <dt>{{ t('billing.automatic.snapshotCount') }}</dt>
            <dd>{{ source.snapshot_count }}</dd>
          </div>
          <div v-if="source.snapshot_state" class="automatic-snapshot">
            <dt>{{ t('billing.automatic.snapshotState') }}</dt>
            <dd :data-snapshot-state="source.snapshot_state">
              {{ t(`billing.automatic.snapshots.${source.snapshot_state}`) }}
            </dd>
          </div>
          <div>
            <dt>{{ t('billing.automatic.models') }}</dt>
            <dd>{{ source.model_count }}</dd>
          </div>
          <div v-if="source.rejected_count !== undefined">
            <dt>{{ t('billing.automatic.rejected') }}</dt>
            <dd>{{ source.rejected_count }}</dd>
          </div>
          <div>
            <dt>{{ t('billing.automatic.checked') }}</dt>
            <dd>
              {{
                source.checked_at
                  ? formatSystemTimestamp(source.checked_at, locale)
                  : t('billing.automatic.never')
              }}
            </dd>
          </div>
          <div>
            <dt>{{ t('billing.automatic.updated') }}</dt>
            <dd>
              {{
                source.updated_at
                  ? formatSystemTimestamp(source.updated_at, locale)
                  : t('billing.automatic.never')
              }}
            </dd>
          </div>
          <div class="automatic-hash">
            <dt>SHA</dt>
            <dd>{{ source.hash || '—' }}</dd>
          </div>
        </dl>
        <p v-if="source.last_error" class="admin-error" role="alert">
          {{ source.last_error }}
        </p>
      </article>
      <p v-if="!status.sources.length" class="admin-muted">
        {{ t('billing.automatic.noSources') }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.automatic-pricing {
  display: grid;
  gap: 0.9rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  padding: 1rem;
  background: var(--surface-solid);
  min-width: 0;
}
.automatic-heading,
.automatic-actions,
.automatic-config,
.automatic-source-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.75rem;
}
.automatic-heading,
.automatic-source-title {
  justify-content: space-between;
}
.automatic-heading h2 {
  font-size: 1rem;
  font-weight: 650;
  margin-bottom: 0.35rem;
}
.automatic-config {
  align-items: end;
}
.automatic-config .admin-field {
  max-width: 14rem;
}
.automatic-sources {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}
.automatic-source {
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  padding: 0.8rem;
  min-width: 0;
  overflow-wrap: anywhere;
}
.automatic-source h3 {
  font-size: 0.875rem;
  font-weight: 650;
}
.automatic-source-title span,
.automatic-url,
dt {
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
.automatic-url {
  overflow-wrap: anywhere;
  margin: 0.5rem 0;
}
dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}
dd {
  font-size: 0.75rem;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.automatic-snapshot,
.automatic-hash {
  grid-column: 1 / -1;
}
.automatic-hash dd {
  font-family: var(--font-mono);
}
.automatic-task {
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}
.automatic-result {
  grid-template-columns: repeat(auto-fit, minmax(7rem, 1fr));
  margin-top: 0.75rem;
}
.automatic-unconfigured {
  margin-top: 0.75rem;
}
.automatic-unconfigured summary {
  cursor: pointer;
}
.automatic-unconfigured ul,
.automatic-errors {
  display: grid;
  gap: 0.35rem;
  padding-left: 1rem;
  margin-top: 0.5rem;
}
@media (max-width: 767px) {
  .automatic-sources {
    grid-template-columns: minmax(0, 1fr);
  }
  .automatic-config .admin-field {
    max-width: none;
    width: 100%;
  }
}
</style>
