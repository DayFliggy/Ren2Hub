<script setup lang="ts">
import { computed, onMounted, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { api } from '@/api/client'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import { registerSettingsDraft } from './settingsDraftGuard'
const { t } = useI18n({
  messages: {
    'zh-CN': {
      policy: {
        title: '请求策略',
        edit: '查看和编辑策略',
        hint: '统一管理上游重试、渠道自动禁用及会话亲和策略。值保留为字符串；保存由服务端校验并原子更新。',
        save: '保存请求策略',
        reload: '重新读取',
      },
    },
    en: {
      policy: {
        title: 'Request policy',
        edit: 'View and edit policy',
        hint: 'Manage retries, automatic channel disabling and session affinity. Values remain strings; the server validates and atomically applies changes.',
        save: 'Save request policy',
        reload: 'Reload',
      },
    },
  },
})
const { t: globalT } = useI18n({ useScope: 'global' })
const settings = useSystemSettings()
const toast = useToast()
const editorId = useId()
const draft = ref(''),
  error = ref(''),
  loading = ref(false),
  baseline = ref<Record<string, string>>({}),
  ready = ref(false),
  request = useLatestRequest()
const baselineText = computed(() => JSON.stringify(baseline.value, null, 2))
const dirty = computed(() => ready.value && draft.value !== baselineText.value)
registerSettingsDraft({ dirty: () => dirty.value, busy: () => loading.value })
function validOptions(value: unknown): Record<string, string> {
  if (
    !value ||
    Array.isArray(value) ||
    typeof value !== 'object' ||
    Object.values(value).some((v) => typeof v !== 'string')
  )
    throw new Error(t('policy.hint'))
  return value as Record<string, string>
}
// Only edited keys are sent, so a stale draft never rewrites values that the
// settings form saved after this panel loaded (upstream web sends the delta).
function changedOptions(next: Record<string, string>) {
  return Object.fromEntries(
    Object.entries(next).filter(([key, value]) => baseline.value[key] !== value)
  )
}
async function run(save: boolean) {
  if (loading.value) return
  if (
    !save &&
    dirty.value &&
    !window.confirm(globalT('systemSettings.discardDraft'))
  )
    return
  let payload: Record<string, string> | undefined
  error.value = ''
  try {
    if (save) {
      if (!ready.value) return
      payload = changedOptions(validOptions(JSON.parse(draft.value)))
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    return
  }
  if (payload && Object.keys(payload).length === 0) {
    toast.info(globalT('systemSettings.noChanges'))
    return
  }
  loading.value = true
  const result = await request.run(async (signal) => {
    const response = save
      ? await api.patch<{ options: Record<string, string> }>(
          '/api/option/request_policy',
          { options: payload },
          { signal }
        )
      : await api.get<{ options: Record<string, string> }>(
          '/api/option/request_policy',
          undefined,
          { signal }
        )
    return validOptions(response.options)
  })
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  baseline.value = result.value
  draft.value = baselineText.value
  ready.value = true
  // The settings form shares these keys; load reports its own failure.
  if (save) {
    await settings.refreshStatus(true)
    await settings.load(true)
  }
}
const load = () => run(false),
  save = () => run(true)
onMounted(load)
// Pick up saves from the settings form unless the admin is editing here.
watch(settings.rawOptions, () => {
  if (ready.value && draft.value === baselineText.value) void load()
})
</script>
<template>
  <RouterLink
    to="/request-processing"
    class="mb-3 inline-block text-sm text-[var(--accent)] underline"
    >{{ globalT('pluginExtensions.title') }}</RouterLink
  >
  <ConsoleCard :title="t('policy.title')" class="mt-6">
    <ErrorBanner
      v-if="error"
      :id="`${editorId}-error`"
      class="mt-3"
      :message="error"
      @retry="load"
    />
    <details class="mt-3" :open="Boolean(error) || dirty" data-policy-editor>
      <summary
        class="cursor-pointer rounded-lg text-sm font-semibold text-[var(--text-secondary)] focus-ring"
      >
        {{ t('policy.edit') }}
      </summary>
      <p
        :id="`${editorId}-hint`"
        class="mt-3 text-sm leading-6 text-[var(--text-secondary)]"
      >
        {{ t('policy.hint') }}
      </p>
      <textarea
        v-model="draft"
        :disabled="loading"
        rows="18"
        :aria-label="t('policy.title')"
        :aria-invalid="Boolean(error)"
        :aria-describedby="`${editorId}-hint${error ? ` ${editorId}-error` : ''}`"
        spellcheck="false"
        class="mt-3 w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-sm text-[var(--text-primary)] focus-ring"
      />
      <div class="mt-4 flex flex-wrap gap-3">
        <ConsoleButton
          :disabled="loading || !ready || draft === baselineText"
          @click="save"
          >{{ t('policy.save') }}</ConsoleButton
        ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
          t('policy.reload')
        }}</ConsoleButton>
      </div>
    </details>
  </ConsoleCard>
</template>
