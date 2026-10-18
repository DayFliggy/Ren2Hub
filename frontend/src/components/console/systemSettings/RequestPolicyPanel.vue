<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { api } from '@/api/client'
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
const draft = ref(''),
  error = ref(''),
  loading = ref(false),
  baseline = ref(''),
  ready = ref(false),
  request = useLatestRequest()
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
async function run(save: boolean) {
  if (loading.value) return
  let payload: Record<string, string> | undefined
  error.value = ''
  try {
    if (save) {
      if (!ready.value) return
      payload = validOptions(JSON.parse(draft.value))
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
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
  if (result.ok) {
    draft.value = JSON.stringify(result.value, null, 2)
    baseline.value = draft.value
    ready.value = true
  } else
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
}
const load = () => run(false),
  save = () => run(true)
onMounted(load)
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
        :disabled="loading || !ready || draft === baseline"
        @click="save"
        >{{ t('policy.save') }}</ConsoleButton
      ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
        t('policy.reload')
      }}</ConsoleButton>
    </div></ConsoleCard
  >
</template>
