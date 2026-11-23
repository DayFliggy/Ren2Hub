<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { onClickOutside, useEventListener } from '@vueuse/core'
import { storeToRefs } from 'pinia'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ExternalLink,
  RefreshCw,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { publicApi } from '@/api/public'
import {
  RESTART_DEADLINE_MS,
  systemUpdateApi,
  watchServerRestart,
  type RestartExpectation,
} from '@/api/systemUpdate'
import { ApiError } from '@/api/types'
import {
  tabLeavesPanel,
  useFloatingPanel,
} from '@/composables/useFloatingPanel'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import MarkdownDocument from '@/components/common/MarkdownDocument.vue'
import { systemUpdateMessages } from '@/i18n/systemUpdate'
import { useAppStore } from '@/stores/app'
import { useSystemUpdateStore } from '@/stores/systemUpdate'
import { hasOpenModalDialog } from '@/utils/modalStack'
import { updateErrorMessage, type ErrorContext } from './errorMessage'
import { displayVersion, publishedDate } from './format'
import VersionRollback from './VersionRollback.vue'
import './version.css'

/**
 * Root version badge after Sub2API's VersionBadge: a status dot at the
 * top-right corner of the brand name, or of the logo when the name is hidden.
 * Its colour and label give the state; the panel shows the version and one
 * state at a time. Operations live here, not in the panel, so closing the
 * panel never cancels them and reopening shows their progress or result.
 * The top bar mounts it only for Root and keys it by account, so a lost role
 * or another account unmounts it and ends everything the old one started.
 * The panel is teleported to <body>: the sidebar clips its own overflow.
 */
const { t, locale } = useI18n({
  useScope: 'local',
  messages: systemUpdateMessages,
})

type Operation = 'update' | 'rollback' | 'restart'

const app = useAppStore()
const releases = useSystemUpdateStore()
const { info, loading, error: checkError } = storeToRefs(releases)

const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const open = ref(false)
const operation = ref<Operation | null>(null)
const actionError = ref('')
const restartNeeded = ref<{
  message: string
  expectation: RestartExpectation
} | null>(null)
const restartState = ref<'waiting' | 'unknown' | null>(null)
const confirmUpdate = ref(false)
const confirmRestart = ref(false)
const rollbackOpen = ref(false)
const notesOpen = ref(false)

let restartController: AbortController | undefined
let disposed = false

const isRelease = computed(() => info.value?.build_type === 'release')
const busy = computed(
  () =>
    loading.value ||
    operation.value !== null ||
    restartState.value === 'waiting'
)
const current = computed(() =>
  displayVersion(info.value?.current_version || app.versionLabel)
)
const hasUpdate = computed(
  () => Boolean(info.value?.has_update) && !restartNeeded.value
)
const release = computed(() => info.value?.release_info ?? null)
const checkFailure = computed(() => {
  if (!checkError.value) return ''
  const { key, params } = updateErrorMessage(checkError.value, 'read')
  return t(key, params ?? {})
})
/** The one main state the panel shows, highest priority first. A failed
 *  check still shows the pending restart under the error. */
const state = computed(() => {
  if (restartState.value) return 'restarting'
  if (actionError.value) return 'action-error'
  if (checkFailure.value) return 'check-error'
  if (restartNeeded.value) return 'restart-needed'
  if (!info.value) return 'loading'
  if (info.value.has_update) return isRelease.value ? 'update' : 'source-update'
  return 'current'
})

/** The dot's colour: neutral while current or loading, warning while
 *  something waits for the admin, danger after a failure. */
const tone = computed(() => {
  if (state.value === 'action-error' || state.value === 'check-error')
    return 'danger'
  if (state.value === 'current' || state.value === 'loading') return 'neutral'
  return 'warning'
})
const badgeLabel = computed(() => {
  const version = current.value
  const latest = displayVersion(info.value?.latest_version ?? '')
  const key = {
    restarting:
      restartState.value === 'waiting'
        ? 'badgeRestarting'
        : 'badgeRestartUnknown',
    'action-error': 'badgeActionFailed',
    'check-error': 'badgeCheckFailed',
    'restart-needed': 'badgeRestart',
    loading: 'badgeChecking',
    update: 'badgeUpdate',
    'source-update': 'badgeUpdate',
    current: 'badgeCurrent',
  }[state.value]
  return t(key, { version, latest })
})

function message(cause: unknown, context: ErrorContext) {
  const { key, params } = updateErrorMessage(cause, context)
  return t(key, params ?? {})
}

void releases.check(false)

const { style: floatingStyle, scope: panelScope } = useFloatingPanel(
  trigger,
  panel,
  open,
  { gap: 4 }
)
// Just above the bar the badge hangs from and below every modal layer, so
// a confirmation opened from the panel covers it.
const panelStyle = computed(() => ({
  ...floatingStyle.value,
  zIndex: 'calc(var(--z-topbar) + 1)',
}))

async function toggle() {
  if (open.value) return close()
  open.value = true
  // The teleported panel is not next to the badge in tab order.
  await nextTick()
  panel.value?.focus()
}

function close() {
  open.value = false
}

function closeToBadge() {
  close()
  void nextTick(() => trigger.value?.focus())
}

// Clicks inside a confirmation dialog opened from the panel stay inside.
onClickOutside(root, close, { ignore: [panel, '[aria-modal="true"]'] })

// Escape closes the panel wherever focus is, as in UserMenu. Any open modal
// (a confirmation from this panel, search, a drawer) sits above the panel and
// handles its own Escape first. The modal stack, not the DOM, says which are
// open: a closing dialog keeps its node through the leave transition.
useEventListener(document, 'keydown', (event: KeyboardEvent) => {
  if (event.key !== 'Escape' || !open.value || event.defaultPrevented) return
  if (hasOpenModalDialog()) return
  event.preventDefault()
  closeToBadge()
})

/** Tab past either end of the panel returns to the badge. */
function onPanelKeydown(event: KeyboardEvent) {
  const element = panel.value
  if (event.key !== 'Tab' || !element) return
  const fromPanel = event.shiftKey && document.activeElement === element
  if (!fromPanel && !tabLeavesPanel(event, element)) return
  event.preventDefault()
  closeToBadge()
}

/** Clears an action failure and reads the server again: after a failed or
 *  unanswered operation, the current state is what the next step needs. */
function refresh() {
  if (busy.value) return
  actionError.value = ''
  void releases.check(true)
}

function startAction(kind: Operation) {
  operation.value = kind
  actionError.value = ''
}

async function update() {
  if (operation.value) return
  startAction('update')
  try {
    const outcome = await systemUpdateApi.update()
    if (disposed) return
    confirmUpdate.value = false
    releases.clear()
    if (outcome.kind === 'restart') {
      // The answer does not name the installed release: GitHub may have
      // published a newer one since the check.
      restartNeeded.value = {
        message: t('updated'),
        expectation: { kind: 'new-version' },
      }
      return
    }
    operation.value = null
    await releases.check(true)
  } catch (cause) {
    if (disposed) return
    confirmUpdate.value = false
    actionError.value = message(cause, 'action')
  } finally {
    if (!disposed && operation.value === 'update') operation.value = null
  }
}

async function rollback(version: string) {
  if (operation.value) return
  startAction('rollback')
  try {
    const outcome = await systemUpdateApi.rollback(version)
    if (disposed) return
    rollbackOpen.value = false
    releases.clear()
    restartNeeded.value = {
      message: t('rolledBack', { version: displayVersion(outcome.version) }),
      expectation: { kind: 'version', version: outcome.version },
    }
  } catch (cause) {
    if (!disposed) actionError.value = message(cause, 'action')
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
  if (operation.value) return
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
      actionError.value = message(cause, 'action')
    }
  } finally {
    if (!stale()) operation.value = null
  }
}

function reload() {
  window.location.reload()
}

onBeforeUnmount(() => {
  disposed = true
  restartController?.abort()
})
</script>
<template>
  <div ref="root" class="relative" data-version-root>
    <button
      ref="trigger"
      type="button"
      class="version-badge"
      data-version-badge
      :data-version-tone="tone"
      :aria-label="badgeLabel"
      aria-haspopup="dialog"
      :aria-expanded="open"
      @click="toggle"
    >
      <span class="version-badge-dot" aria-hidden="true" />
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        class="version-panel subtle-scroll animate-scale-in"
        role="dialog"
        aria-modal="false"
        :aria-label="t('title')"
        tabindex="-1"
        :style="panelStyle"
        data-handdrawn="menu"
        data-floating-panel
        :data-handdrawn-scope="panelScope"
        data-version-panel
        :data-version-state="state"
        @keydown="onPanelKeydown"
      >
        <header class="version-panel-head">
          <h2>{{ t('title') }}</h2>
          <button
            type="button"
            class="version-icon-button"
            :aria-label="t('refresh')"
            :title="t('refresh')"
            :disabled="busy"
            data-version-refresh
            @click="refresh"
          >
            <RefreshCw
              :size="15"
              :class="{ 'animate-spin': loading }"
              aria-hidden="true"
            />
          </button>
        </header>

        <div class="version-current">
          <p class="version-current-value" data-version-current>
            {{ current }}
          </p>
          <p
            v-if="hasUpdate && info"
            class="version-current-sub"
            data-version-latest
          >
            {{
              t('latestLine', { version: displayVersion(info.latest_version) })
            }}
          </p>
        </div>
        <p
          v-if="info?.warning"
          class="version-warning-line"
          role="status"
          data-version-warning
        >
          {{ t('cachedWarning') }}
        </p>

        <div v-if="state === 'restarting'" class="version-state" role="status">
          <p v-if="restartState === 'waiting'" class="version-note">
            {{ t('restarting') }}
          </p>
          <template v-else>
            <p class="version-note" data-version-restart-unknown>
              {{ t('restartUnknown') }}
            </p>
            <ConsoleButton size="sm" variant="secondary" @click="reload">{{
              t('reload')
            }}</ConsoleButton>
          </template>
        </div>

        <div
          v-else-if="state === 'action-error' || state === 'check-error'"
          class="version-state version-error"
          role="alert"
          data-version-error
        >
          <p class="version-error-title">
            <AlertTriangle :size="15" aria-hidden="true" />
            <span>{{
              state === 'action-error' ? t('actionFailed') : t('checkFailed')
            }}</span>
          </p>
          <p class="version-note">
            {{ state === 'action-error' ? actionError : checkFailure }}
          </p>
          <ConsoleButton
            size="sm"
            variant="secondary"
            :disabled="busy"
            data-version-retry
            @click="refresh"
            >{{
              state === 'action-error' ? t('refreshInfo') : t('retry')
            }}</ConsoleButton
          >
        </div>

        <p v-else-if="state === 'loading'" class="version-note" role="status">
          {{ t('checking') }}
        </p>

        <div
          v-else-if="state === 'source-update'"
          class="version-state"
          data-version-source-update
        >
          <a
            v-if="release"
            class="version-link"
            :href="release.html_url"
            target="_blank"
            rel="noopener noreferrer"
            data-version-release-link
            >{{ t('viewRelease') }} <ExternalLink :size="13" aria-hidden="true"
          /></a>
          <p class="version-note">{{ t('sourceBuild') }}</p>
        </div>

        <div
          v-else-if="state === 'update' && info"
          class="version-state version-available"
          data-version-update
        >
          <p class="version-available-head">
            <span class="version-available-tag">{{
              displayVersion(info.latest_version)
            }}</span>
            <span
              v-if="release && publishedDate(release.published_at, locale)"
              class="version-note"
              >{{
                t('published', {
                  date: publishedDate(release.published_at, locale),
                })
              }}</span
            >
          </p>
          <p v-if="operation === 'update'" class="version-note" role="status">
            {{ t('updating') }}
          </p>
          <ConsoleButton
            size="sm"
            class="w-full"
            :disabled="busy"
            :loading="operation === 'update'"
            data-version-update-now
            @click="confirmUpdate = true"
            >{{ t('updateNow') }}</ConsoleButton
          >
          <template v-if="release">
            <div class="version-row">
              <a
                class="version-link"
                :href="release.html_url"
                target="_blank"
                rel="noopener noreferrer"
                data-version-release-link
                >{{ t('changelog') }}
                <ExternalLink :size="13" aria-hidden="true"
              /></a>
              <button
                v-if="release.body"
                type="button"
                class="version-toggle"
                :aria-expanded="notesOpen"
                data-version-notes-toggle
                @click="notesOpen = !notesOpen"
              >
                {{ notesOpen ? t('hideNotes') : t('showNotes') }}
                <ChevronDown
                  :size="14"
                  :class="{ 'rotate-180': notesOpen }"
                  aria-hidden="true"
                />
              </button>
            </div>
            <MarkdownDocument
              v-if="notesOpen && release.body"
              class="version-notes subtle-scroll"
              :markdown="release.body"
              :base-url="release.html_url"
              data-version-notes
            />
          </template>
        </div>

        <div
          v-else-if="state === 'current' && info"
          class="version-state"
          data-version-up-to-date
        >
          <p class="version-ok">
            <CheckCircle2 :size="16" aria-hidden="true" />
            <span>{{ t('upToDate') }}</span>
          </p>
          <a
            v-if="release"
            class="version-link"
            :href="release.html_url"
            target="_blank"
            rel="noopener noreferrer"
            data-version-release-link
            >{{ t('viewRelease') }} <ExternalLink :size="13" aria-hidden="true"
          /></a>
          <button
            type="button"
            class="version-toggle"
            :aria-expanded="rollbackOpen"
            data-version-rollback-toggle
            @click="rollbackOpen = !rollbackOpen"
          >
            {{ t('rollback') }}
            <ChevronDown
              :size="14"
              :class="{ 'rotate-180': rollbackOpen }"
              aria-hidden="true"
            />
          </button>
          <p v-if="operation === 'rollback'" class="version-note" role="status">
            {{ t('rollingBack') }}
          </p>
          <VersionRollback
            v-if="rollbackOpen"
            :release="isRelease"
            :busy="busy"
            @rollback="rollback"
          />
        </div>

        <div
          v-if="
            restartNeeded &&
            (state === 'restart-needed' || state === 'check-error')
          "
          class="version-state version-success"
          role="status"
          data-version-restart-needed
        >
          <p>{{ restartNeeded.message }}</p>
          <ConsoleButton
            size="sm"
            :disabled="busy"
            data-version-restart
            @click="confirmRestart = true"
            >{{ t('restart') }}</ConsoleButton
          >
        </div>

        <footer
          v-if="isRelease && !restartNeeded && !restartState"
          class="version-panel-foot"
        >
          <button
            type="button"
            class="block min-h-11 w-full rounded-none px-4 py-2 text-left text-sm text-[var(--text-primary)] transition-colors hover:bg-[var(--surface-muted)] focus-ring disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="busy"
            data-version-restart-service
            @click="confirmRestart = true"
          >
            {{ t('restartService') }}
          </button>
        </footer>
      </div>
    </Teleport>

    <ConfirmDialog
      :open="confirmUpdate"
      :title="
        t('confirmUpdate', {
          version: displayVersion(info?.latest_version ?? ''),
        })
      "
      :message="t('confirmUpdateMessage')"
      :confirm-text="t('confirmUpdateAction')"
      :loading="operation === 'update'"
      @confirm="update"
      @cancel="confirmUpdate = false"
      ><p v-if="operation === 'update'" class="version-note mt-3">
        {{ t('updating') }}
      </p></ConfirmDialog
    >
    <ConfirmDialog
      :open="confirmRestart"
      :title="t('confirmRestart')"
      :message="t('confirmRestartMessage')"
      :confirm-text="t('restart')"
      @confirm="restart"
      @cancel="confirmRestart = false"
    />
  </div>
</template>
