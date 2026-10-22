<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { api } from '@/api/client'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
const { t } = useI18n({
  messages: {
    'zh-CN': {
      policy: {
        title: '请求策略',
        hint: '统一管理上游重试、敏感词、渠道自动禁用及会话亲和策略。值保留为字符串；保存由服务端校验并原子更新。',
        save: '保存请求策略',
        reload: '重新读取',
      },
    },
    en: {
      policy: {
        title: 'Request policy',
        hint: 'Manage retries, sensitive words, automatic channel disabling and session affinity. Values remain strings; the server validates and atomically applies changes.',
        save: 'Save request policy',
        reload: 'Reload',
      },
    },
  },
})
const { t: globalT } = useI18n({ useScope: 'global' })
const settings = useSystemSettings()
const toast = useToast()
const draft = ref(''),
  error = ref(''),
  loading = ref(false),
  baseline = ref<Record<string, string>>({}),
  ready = ref(false),
  request = useLatestRequest()
const baselineText = computed(() => JSON.stringify(baseline.value, null, 2))
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
  if (save) await settings.load(true)
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
  <ConsoleCard :title="t('policy.title')" class="mt-6"
    ><p class="mb-4 text-sm">{{ t('policy.hint') }}</p>
    <ErrorBanner v-if="error" :message="error" /><textarea
      v-model="draft"
      :disabled="loading"
      rows="18"
      :aria-label="t('policy.title')"
      spellcheck="false"
      class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3 font-mono text-sm"
    />
    <div class="mt-4 flex gap-3">
      <ConsoleButton
        :disabled="loading || !ready || draft === baselineText"
        @click="save"
        >{{ t('policy.save') }}</ConsoleButton
      ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
        t('policy.reload')
      }}</ConsoleButton>
    </div></ConsoleCard
  >
</template>
