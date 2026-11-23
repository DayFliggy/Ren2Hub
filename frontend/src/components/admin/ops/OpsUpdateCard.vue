<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { AlertTriangle, ExternalLink } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { publicApi } from '@/api/public'
import {
  RESTART_DEADLINE_MS,
  systemUpdateApi,
  watchServerRestart,
  type RestartExpectation,
  type RollbackVersion,
  type SystemVersion,
  type UpdateCheck,
} from '@/api/systemUpdate'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import MarkdownDocument from '@/components/common/MarkdownDocument.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { formatTimestamp } from '@/utils/opsFormat'
import OpsCard from './OpsCard.vue'
import OpsMetric from './OpsMetric.vue'

/**
 * Root-only release panel following Sub2API's flow: check GitHub, update or
 * roll back the binary, then restart and wait for the new process. Server
 * error text is English and never shown; failures map by HTTP status.
 */
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const { t: tGlobal } = useI18n({ useScope: 'global' })

type Operation = 'update' | 'rollback' | 'restart'
type ErrorContext = 'read' | 'action'

const version = ref<SystemVersion | null>(null)
const versionLoading = ref(false)
const versionError = ref('')
const versionUpdatedAt = ref(0)
const check = ref<UpdateCheck | null>(null)
const checking = ref(false)
const operation = ref<Operation | null>(null)
const error = ref('')
const result = ref('')
const restartNeeded = ref<{
  message: string
  expectation: RestartExpectation
} | null>(null)
const restartState = ref<'waiting' | 'unknown' | null>(null)
const confirmUpdate = ref(false)
const confirmRestart = ref(false)
const rollbackOpen = ref(false)
const rollbackVersions = ref<RollbackVersion[] | null>(null)
const rollbackLoading = ref(false)
const rollbackError = ref('')
const rollbackTarget = ref('')

let readController: AbortController | undefined
let rollbackController: AbortController | undefined
let restartController: AbortController | undefined
let disposed = false

const isRelease = computed(() => version.value?.build_type === 'release')
const busy = computed(
  () =>
    checking.value ||
    operation.value !== null ||
    restartState.value === 'waiting'
)
const currentVersion = computed(
  () => check.value?.current_version ?? version.value?.version ?? '--'
)
const release = computed(() => check.value?.release_info ?? null)
const showNotes = computed(() =>
  Boolean(check.value?.has_update && release.value)
)
const rollbackOptions = computed(() =>
  (rollbackVersions.value ?? []).map((item) => ({
    value: item.version,
    label: `${display(item.version)} · ${published(item.published_at)}`,
  }))
)

function display(value: string) {
  return /^\d/.test(value) ? `v${value}` : value
}

function published(value: string) {
  return formatTimestamp(Date.parse(value) / 1000, locale.value)
}

function message(cause: unknown, context: ErrorContext): string {
  if (!(cause instanceof ApiError)) return t('update.errors.generic')
  if (cause.code === 'INVALID_RESPONSE') return t('update.errors.invalid')
  // The transport reports a sign-in change during the request as HTTP 409.
  if (cause.code === 'AUTH_SESSION_CHANGED')
    return t('update.errors.sessionChanged')
  switch (cause.status) {
    case 400:
      return t('update.errors.rejected')
    case 404:
      return t('update.errors.notInList')
    case 409:
      return t('update.errors.busy')
    case 500:
      if (context === 'action') return t('update.errors.server')
      break
    case 502:
      return t(
        context === 'action' ? 'update.errors.download' : 'update.errors.github'
      )
    case 504:
    case undefined:
      // A gateway timeout or a dropped connection leaves the outcome open.
      return t(
        context === 'action'
          ? 'update.errors.noResponse'
          : 'update.errors.unreachable'
      )
  }
  return t('update.errors.status', { status: cause.status })
}

function nextReadSignal() {
  readController?.abort()
  readController = new AbortController()
  return readController.signal
}

async function loadVersion() {
  const signal = nextReadSignal()
  versionLoading.value = true
  versionError.value = ''
  try {
    const value = await systemUpdateApi.version(signal)
    if (signal.aborted) return
    version.value = value
    versionUpdatedAt.value = Date.now()
  } catch (cause) {
    if (!signal.aborted) versionError.value = message(cause, 'read')
    return
  } finally {
    if (!signal.aborted) versionLoading.value = false
  }
  if (isRelease.value) await checkUpdates(false)
}

async function checkUpdates(force: boolean) {
  const signal = nextReadSignal()
  checking.value = true
  error.value = ''
  try {
    const value = await systemUpdateApi.checkUpdates(force, signal)
    if (!signal.aborted) check.value = value
  } catch (cause) {
    if (!signal.aborted) error.value = message(cause, 'read')
  } finally {
    if (!signal.aborted) checking.value = false
  }
}

function startAction(kind: Operation) {
  operation.value = kind
  error.value = ''
  result.value = ''
}

async function update() {
  startAction('update')
  try {
    const outcome = await systemUpdateApi.update()
    if (disposed) return
    confirmUpdate.value = false
    if (outcome.kind === 'restart') {
      // The answer does not name the installed release: GitHub may have
      // published a newer one since the check.
      restartNeeded.value = {
        message: t('update.updated'),
        expectation: { kind: 'new-version' },
      }
      return
    }
    result.value = t('update.alreadyLatest', {
      version: display(outcome.latest_version),
    })
    operation.value = null
    await checkUpdates(true)
  } catch (cause) {
    if (disposed) return
    confirmUpdate.value = false
    error.value = message(cause, 'action')
  } finally {
    if (!disposed && operation.value === 'update') operation.value = null
  }
}

async function openRollback() {
  rollbackOpen.value = true
  rollbackTarget.value = ''
  rollbackError.value = ''
  rollbackVersions.value = null
  rollbackController?.abort()
  const request = new AbortController()
  rollbackController = request
  rollbackLoading.value = true
  try {
    const versions = await systemUpdateApi.rollbackVersions(request.signal)
    if (!request.signal.aborted) rollbackVersions.value = versions
  } catch (cause) {
    if (!request.signal.aborted) rollbackError.value = message(cause, 'read')
  } finally {
    if (!request.signal.aborted) rollbackLoading.value = false
  }
}

function closeRollback() {
  if (operation.value === 'rollback') return
  rollbackController?.abort()
  rollbackLoading.value = false
  rollbackOpen.value = false
}

async function rollback() {
  if (!rollbackTarget.value || operation.value) return
  startAction('rollback')
  rollbackError.value = ''
  try {
    const outcome = await systemUpdateApi.rollback(rollbackTarget.value)
    if (disposed) return
    rollbackOpen.value = false
    restartNeeded.value = {
      message: t('update.rolledBack', { version: display(outcome.version) }),
      expectation: { kind: 'version', version: outcome.version },
    }
  } catch (cause) {
    if (!disposed) rollbackError.value = message(cause, 'action')
  } finally {
    if (!disposed) operation.value = null
  }
}

/** Only an answered error refuses the restart. A dropped connection leaves
 *  the outcome unknown, so the status poll decides. */
function refusedRestart(cause: unknown) {
  return !(cause instanceof ApiError) || cause.status !== undefined
}

async function restart() {
  confirmRestart.value = false
  startAction('restart')
  restartController?.abort()
  const request = new AbortController()
  restartController = request
  const stale = () => disposed || request.signal.aborted
  try {
    const baseline = await publicApi.status(request.signal)
    if (stale()) return
    const deadline = Date.now() + RESTART_DEADLINE_MS
    try {
      await systemUpdateApi.restart()
    } catch (cause) {
      if (refusedRestart(cause)) throw cause
    }
    if (stale()) return
    restartState.value = 'waiting'
    const outcome = await watchServerRestart(
      baseline,
      restartNeeded.value?.expectation ?? { kind: 'new-process' },
      deadline,
      request.signal
    )
    if (stale()) return
    if (outcome === 'restarted') window.location.reload()
    else restartState.value = 'unknown'
  } catch (cause) {
    if (!stale()) {
      restartState.value = null
      error.value = message(cause, 'action')
    }
  } finally {
    if (!stale()) operation.value = null
  }
}

function reload() {
  window.location.reload()
}

onMounted(loadVersion)
onBeforeUnmount(() => {
  disposed = true
  readController?.abort()
  rollbackController?.abort()
  restartController?.abort()
})
</script>
<template>
  <OpsCard
    :title="t('update.title')"
    :scope="t('rootOnly')"
    :loading="versionLoading"
    :loaded="version !== null"
    :error="versionError"
    :updated-at="versionUpdatedAt"
    data-ops-section="update"
    @retry="loadVersion"
  >
    <template v-if="isRelease && check" #action>
      <StatusChip :tone="check.has_update ? 'accent' : 'success'">{{
        check.has_update ? t('update.available') : t('update.upToDate')
      }}</StatusChip>
    </template>
    <template v-if="version">
      <dl class="ops-metric-grid ops-metric-grid--3">
        <OpsMetric
          :label="t('update.current')"
          :value="display(currentVersion)"
          data-update-current
        />
        <OpsMetric
          v-if="isRelease && check"
          :label="t('update.latest')"
          :value="display(check.latest_version || '--')"
          data-update-latest
        />
        <OpsMetric
          v-if="showNotes && release"
          :label="t('update.published')"
          :value="published(release.published_at)"
          size="sm"
          class="ops-update-published"
          data-update-published
        />
      </dl>
      <p v-if="!isRelease" class="ops-note">{{ t('update.sourceBuild') }}</p>
      <p
        v-if="check?.warning"
        class="ops-update-warning"
        role="status"
        data-update-warning
      >
        {{ t('update.cachedWarning') }}
      </p>
      <div v-if="error" class="ops-inline-error" role="alert" data-update-error>
        <AlertTriangle :size="15" aria-hidden="true" />
        <p class="ops-inline-error-title min-w-0 flex-1">{{ error }}</p>
      </div>
      <p v-if="result" class="ops-result" role="status" data-update-result>
        {{ result }}
      </p>
      <div
        v-if="restartNeeded && !restartState"
        class="ops-result ops-update-banner"
        role="status"
        data-update-restart-needed
      >
        <span>{{ restartNeeded.message }}</span>
        <ConsoleButton
          size="sm"
          :disabled="busy"
          data-update-action="restart"
          @click="confirmRestart = true"
          >{{ t('update.restart') }}</ConsoleButton
        >
      </div>
      <div
        v-if="restartState"
        class="ops-update-banner ops-update-restart"
        role="status"
        data-update-restart-state
      >
        <span v-if="restartState === 'waiting'">{{
          t('update.restarting')
        }}</span>
        <template v-else>
          <span>{{ t('update.restartUnknown') }}</span>
          <ConsoleButton size="sm" variant="secondary" @click="reload">{{
            t('update.reload')
          }}</ConsoleButton>
        </template>
      </div>
      <div v-if="isRelease && !restartNeeded" class="ops-update-actions">
        <ConsoleButton
          variant="secondary"
          size="sm"
          :loading="checking"
          :disabled="busy"
          data-update-action="check"
          @click="checkUpdates(true)"
          >{{ t('update.check') }}</ConsoleButton
        >
        <ConsoleButton
          v-if="check?.has_update"
          size="sm"
          :disabled="busy"
          data-update-action="update"
          @click="confirmUpdate = true"
          >{{
            t('update.updateTo', { version: display(check.latest_version) })
          }}</ConsoleButton
        >
        <ConsoleButton
          variant="secondary"
          size="sm"
          :disabled="busy"
          data-update-action="rollback"
          @click="openRollback"
          >{{ t('update.rollback') }}</ConsoleButton
        >
        <ConsoleButton
          variant="ghost"
          size="sm"
          :disabled="busy"
          data-update-action="restart"
          @click="confirmRestart = true"
          >{{ t('update.restart') }}</ConsoleButton
        >
      </div>
      <section v-if="showNotes && release" class="ops-update-notes">
        <div class="ops-update-notes-head">
          <h3>{{ release.name || t('update.releaseNotes') }}</h3>
          <a
            :href="release.html_url"
            target="_blank"
            rel="noopener noreferrer"
            data-update-link
            >{{ t('update.releaseLink') }}
            <ExternalLink :size="13" aria-hidden="true"
          /></a>
        </div>
        <MarkdownDocument
          class="ops-update-notes-body"
          :markdown="release.body"
          :base-url="release.html_url"
          data-update-notes
        />
      </section>
    </template>
  </OpsCard>

  <ConfirmDialog
    :open="confirmUpdate"
    :title="
      t('update.confirmUpdate', {
        version: display(check?.latest_version ?? ''),
      })
    "
    :message="t('update.confirmUpdateMessage')"
    :confirm-text="t('update.confirmUpdateAction')"
    :loading="operation === 'update'"
    @confirm="update"
    @cancel="confirmUpdate = false"
    ><p v-if="operation === 'update'" class="ops-note mt-3">
      {{ t('update.updating') }}
    </p></ConfirmDialog
  >
  <ConfirmDialog
    :open="confirmRestart"
    :title="t('update.confirmRestart')"
    :message="t('update.confirmRestartMessage')"
    :confirm-text="t('update.restart')"
    @confirm="restart"
    @cancel="confirmRestart = false"
  />
  <ConsoleModal
    :open="rollbackOpen"
    :title="t('update.rollbackTitle')"
    :close-disabled="operation === 'rollback'"
    @close="closeRollback"
  >
    <div class="ops-update-rollback">
      <p class="ops-note">{{ t('update.rollbackMessage') }}</p>
      <p v-if="rollbackLoading" class="ops-note">{{ t('loading') }}</p>
      <p
        v-else-if="rollbackVersions && !rollbackVersions.length"
        class="ops-empty"
      >
        {{ t('update.rollbackEmpty') }}
      </p>
      <FilterSelect
        v-else-if="rollbackVersions"
        v-model="rollbackTarget"
        :options="rollbackOptions"
        :label="t('update.rollbackVersion')"
        :placeholder="t('update.rollbackPlaceholder')"
        :disabled="operation === 'rollback'"
      />
      <p v-if="operation === 'rollback'" class="ops-note">
        {{ t('update.rollingBack') }}
      </p>
      <div
        v-if="rollbackError"
        class="ops-inline-error"
        role="alert"
        data-update-error
      >
        <AlertTriangle :size="15" aria-hidden="true" />
        <p class="ops-inline-error-title min-w-0 flex-1">
          {{ rollbackError }}
        </p>
      </div>
    </div>
    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          :disabled="operation === 'rollback'"
          @click="closeRollback"
          >{{ tGlobal('common.cancel') }}</ConsoleButton
        >
        <ConsoleButton
          variant="danger"
          :disabled="!rollbackTarget || busy"
          :loading="operation === 'rollback'"
          data-update-rollback-confirm
          @click="rollback"
          >{{ t('update.rollbackConfirm') }}</ConsoleButton
        >
      </div>
    </template>
  </ConsoleModal>
</template>
