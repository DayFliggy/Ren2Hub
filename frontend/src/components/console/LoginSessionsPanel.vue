<script setup lang="ts">
import { Computer, LogOut, RefreshCw, ShieldCheck } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { authApi } from '@/api/auth'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import type { LoginSession } from '@/types/auth'
import { formatTime, relativeTime } from '@/utils/format'
import { loginMethodLabel, sessionDevice } from '@/utils/loginSession'

const { t, locale } = useI18n()
const toast = useToast()
const router = useRouter()
const auth = useAuthStore()

const sessions = ref<LoginSession[]>([])
const loading = ref(true)
const loadError = ref('')
const target = ref<LoginSession | null>(null)
const confirmOthers = ref(false)
const revoking = ref(false)
const revokingOthers = ref(false)

const hasOtherSessions = computed(() =>
  sessions.value.some((item) => !item.current)
)
const labels = computed(() => ({
  unknown: t('settings.unknown'),
  password: t('settings.loginMethodPassword'),
  twoFactor: t('settings.loginMethodTwoFA'),
  passkey: t('settings.loginMethodPasskey'),
  wechat: t('settings.loginMethodWeChat'),
  telegram: t('settings.loginMethodTelegram'),
  oauth: t('settings.loginMethodOAuth'),
}))

async function loadSessions(): Promise<void> {
  loading.value = true
  loadError.value = ''
  try {
    sessions.value = await authApi.getLoginSessions()
  } catch (error) {
    loadError.value = error instanceof ApiError ? error.message : String(error)
  } finally {
    loading.value = false
  }
}

function requestRevoke(session: LoginSession): void {
  target.value = session
}

async function revokeTarget(): Promise<void> {
  if (!target.value || revoking.value) return
  const session = target.value
  revoking.value = true
  try {
    const result = await authApi.revokeLoginSession(session.sid)
    target.value = null
    if (session.current || result.current) {
      auth.invalidateLocalSession()
      await router.push({ name: 'sign-in' })
      return
    }
    toast.success(t('settings.sessionRevoked'))
    await loadSessions()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    revoking.value = false
  }
}

async function revokeOthers(): Promise<void> {
  if (revokingOthers.value || !hasOtherSessions.value) return
  revokingOthers.value = true
  try {
    await authApi.revokeOtherLoginSessions()
    confirmOthers.value = false
    toast.success(t('settings.otherSessionsRevoked'))
    await loadSessions()
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    revokingOthers.value = false
  }
}

function requestRevokeOthers(): void {
  if (hasOtherSessions.value) confirmOthers.value = true
}

function deviceName(session: LoginSession): string {
  const touchPoints =
    session.current && typeof navigator !== 'undefined'
      ? navigator.maxTouchPoints
      : 0
  return sessionDevice(
    session.user_agent,
    t('settings.unknownDevice'),
    t('settings.browser'),
    touchPoints
  )
}

function lastActive(session: LoginSession): string {
  return relativeTime(session.last_active_at, locale.value)
}

onMounted(() => void loadSessions())
</script>

<template>
  <ConsoleCard :padded="false" class="overflow-hidden">
    <header
      class="flex flex-col gap-4 border-b border-[var(--border-subtle)] px-6 py-6 sm:flex-row sm:items-center sm:justify-between sm:px-7"
    >
      <div class="flex min-w-0 items-start gap-3">
        <ShieldCheck
          :size="20"
          :stroke-width="1.8"
          class="mt-0.5 shrink-0 text-[var(--text-tertiary)]"
          aria-hidden="true"
        />
        <div class="min-w-0">
          <h2 class="text-xl font-semibold text-[var(--text-primary)]">
            {{ t('settings.loginSessions') }}
          </h2>
          <p class="mt-1 text-sm leading-5 text-[var(--text-tertiary)]">
            {{ t('settings.loginSessionsDesc') }}
          </p>
        </div>
      </div>
      <ConsoleButton
        variant="secondary"
        size="sm"
        class="w-full sm:w-auto"
        :disabled="!hasOtherSessions || revokingOthers"
        :loading="revokingOthers"
        @click="requestRevokeOthers"
      >
        <LogOut v-if="!revokingOthers" :size="15" aria-hidden="true" />
        {{ t('settings.revokeOtherSessions') }}
      </ConsoleButton>
    </header>

    <div v-if="loading" class="space-y-3 px-5 py-5 sm:px-6">
      <div
        v-for="n in 2"
        :key="n"
        class="h-20 animate-pulse rounded-xl bg-[var(--surface-muted)]"
      />
    </div>
    <div v-else-if="loadError" class="px-5 py-8 text-center sm:px-6">
      <p class="text-sm text-[var(--status-danger-text)]">{{ loadError }}</p>
      <ConsoleButton
        variant="secondary"
        size="sm"
        class="mt-4"
        @click="loadSessions"
      >
        <RefreshCw :size="15" aria-hidden="true" />
        {{ t('common.retry') }}
      </ConsoleButton>
    </div>
    <div
      v-else-if="sessions.length === 0"
      class="px-5 py-10 text-center sm:px-6"
    >
      <Computer
        :size="28"
        class="mx-auto text-[var(--text-tertiary)]"
        aria-hidden="true"
      />
      <p class="mt-3 text-sm text-[var(--text-tertiary)]">
        {{ t('settings.noActiveSessions') }}
      </p>
    </div>
    <div v-else class="divide-y divide-[var(--border-subtle)] px-5 sm:px-6">
      <div
        v-for="session in sessions"
        :key="session.sid"
        class="flex flex-col gap-4 py-5 sm:flex-row sm:items-center"
      >
        <span class="settings-icon-tile size-10">
          <Computer :size="19" aria-hidden="true" />
        </span>
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <p class="font-semibold text-[var(--text-primary)]">
              {{ deviceName(session) }}
            </p>
            <StatusChip v-if="session.current" tone="info">
              {{ t('settings.currentSession') }}
            </StatusChip>
          </div>
          <p class="mt-1 text-xs text-[var(--text-tertiary)]">
            {{
              t('settings.sessionIpMethod', {
                ip: session.ip || t('settings.unknown'),
                method: loginMethodLabel(session.login_method, labels),
              })
            }}
          </p>
          <p class="mt-1 text-xs text-[var(--text-tertiary)]">
            {{
              t('settings.sessionActivity', {
                time: lastActive(session),
                expires: formatTime(session.expires_at),
              })
            }}
          </p>
        </div>
        <ConsoleButton
          :variant="session.current ? 'secondary' : 'ghost'"
          size="sm"
          class="w-full shrink-0 sm:w-auto"
          @click="requestRevoke(session)"
        >
          {{
            session.current
              ? t('settings.signOutCurrent')
              : t('settings.revokeSession')
          }}
        </ConsoleButton>
      </div>
    </div>
  </ConsoleCard>

  <ConfirmDialog
    :open="Boolean(target)"
    :title="
      target?.current
        ? t('settings.signOutCurrentConfirmTitle')
        : t('settings.revokeSessionConfirmTitle')
    "
    :message="t('settings.revokeSessionConfirm')"
    :confirm-text="
      target?.current
        ? t('settings.signOutCurrent')
        : t('settings.revokeSession')
    "
    :loading="revoking"
    @confirm="revokeTarget"
    @cancel="target = null"
  />

  <ConfirmDialog
    :open="confirmOthers"
    :title="t('settings.revokeOtherSessionsConfirmTitle')"
    :message="t('settings.revokeOtherSessionsConfirm')"
    :confirm-text="t('settings.revokeOtherSessions')"
    :loading="revokingOthers"
    @confirm="revokeOthers"
    @cancel="confirmOthers = false"
  />
</template>

<style scoped>
.settings-icon-tile {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  border-radius: 0.625rem;
  background: var(--accent-soft);
  color: var(--accent-text);
}
</style>
