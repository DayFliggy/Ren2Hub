<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TextInput from '@/components/common/TextInput.vue'
import FormField from '@/components/common/FormField.vue'
import PageHero from '@/components/console/PageHero.vue'

interface AuditRecord {
  id: number
  created_at: number
  username: string
  category: string
  action: string
  success: boolean
  ip: string
  content: string
  token_ref: string
  method: string
  route: string
  status: number
  other?: { op?: { params?: Record<string, unknown> } }
}
const visibleParameterKeys = new Set([
  'id',
  'ids',
  'name',
  'username',
  'status',
  'requested_count',
  'deleted_count',
  'count',
  'delta',
  'event_id',
  'expected_version',
  'operation_id',
  'reason',
  'available_quota',
  'unlimited_quota',
  'method',
  'scope',
  'provider',
])
function visibleParameters(row: AuditRecord) {
  return Object.entries(row.other?.op?.params ?? {}).filter(([key]) =>
    visibleParameterKeys.has(key)
  )
}
const { t } = useI18n()
const auth = useAuthStore()
const canReadAll = computed(
  () => auth.isAdmin && auth.hasPermission('audit', 'read')
)
const all = ref(false)
const category = ref('')
const tokenRef = ref('')
const success = ref('')
const page = ref(1)
const total = ref(0)
const rows = ref<AuditRecord[]>([])
const error = ref('')
const loading = ref(false)
let controller: AbortController | null = null
let sequence = 0
async function load() {
  const current = ++sequence
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    const data = await api.get<{ items: AuditRecord[]; total: number }>(
      all.value && canReadAll.value ? '/api/audit' : '/api/audit/self',
      {
        p: page.value,
        page_size: 20,
        category: category.value,
        token_ref: tokenRef.value.trim(),
        success: success.value,
      },
      { signal: controller.signal }
    )
    if (current !== sequence) return
    rows.value = data.items
    total.value = data.total
  } catch (cause) {
    if (current === sequence && !controller.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (current === sequence) loading.value = false
  }
}
function filter() {
  page.value = 1
  void load()
}
watch([all, page], load, { immediate: true })
onBeforeUnmount(() => {
  sequence++
  controller?.abort()
})
</script>

<template>
  <div class="space-y-5">
    <PageHero
      :title="t('security.auditTitle')"
      :crumbs="[t('security.auditTitle')]"
      ><p class="text-sm text-[var(--text-secondary)]">
        {{ t('security.auditHint') }}
      </p></PageHero
    >
    <ConsoleCard>
      <form
        class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4"
        @submit.prevent="filter"
      >
        <FormField :label="t('security.auditCategory')"
          ><select
            v-model="category"
            class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-page)] p-2"
          >
            <option value="">{{ t('security.auditAll') }}</option>
            <option
              v-for="item in ['login', 'security', 'operation', 'access_token']"
              :key="item"
            >
              {{ item }}
            </option>
          </select></FormField
        >
        <FormField :label="t('security.auditToken')"
          ><TextInput v-model="tokenRef" maxlength="64"
        /></FormField>
        <FormField :label="t('security.auditResult')"
          ><select
            v-model="success"
            class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-page)] p-2"
          >
            <option value="">{{ t('security.auditAll') }}</option>
            <option value="true">{{ t('security.auditSuccess') }}</option>
            <option value="false">{{ t('security.auditFailure') }}</option>
          </select></FormField
        >
        <ConsoleButton type="submit" class="self-end" :loading="loading">{{
          t('common.search')
        }}</ConsoleButton>
        <label v-if="canReadAll" class="flex items-center gap-2 text-sm"
          ><input v-model="all" type="checkbox" />{{
            t('security.auditAdmin')
          }}</label
        >
      </form>
      <p
        v-if="error"
        role="alert"
        class="mt-4 text-sm text-[var(--danger-text)]"
      >
        {{ error }}
      </p>
      <div class="mt-5 overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead>
            <tr>
              <th
                v-for="key in [
                  'auditTime',
                  'auditActor',
                  'auditAction',
                  'auditResult',
                  'auditIP',
                ]"
                :key="key"
                class="whitespace-nowrap border-b border-[var(--border-subtle)] p-3"
              >
                {{ t(`security.${key}`) }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id">
              <td class="whitespace-nowrap p-3">
                {{ new Date(row.created_at * 1000).toLocaleString() }}
              </td>
              <td class="p-3">{{ row.username }}</td>
              <td class="max-w-sm break-words p-3">
                <div>{{ row.action }}</div>
                <div class="mt-1 text-xs text-[var(--text-tertiary)]">
                  {{ row.method }} {{ row.route }} {{ row.status || '' }}
                </div>
                <div class="text-xs text-[var(--text-secondary)]">
                  {{ row.content }}
                </div>
                <details
                  v-if="visibleParameters(row).length"
                  class="mt-2 text-xs"
                >
                  <summary class="cursor-pointer">
                    {{ t('security.auditDetails') }}
                  </summary>
                  <dl
                    class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 break-all"
                  >
                    <template
                      v-for="[key, value] in visibleParameters(row)"
                      :key="key"
                    >
                      <dt>{{ key }}</dt>
                      <dd>{{ value }}</dd>
                    </template>
                  </dl>
                </details>
              </td>
              <td class="p-3">
                {{
                  t(
                    row.success
                      ? 'security.auditSuccess'
                      : 'security.auditFailure'
                  )
                }}
              </td>
              <td class="p-3">{{ row.ip || '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p
        v-if="!loading && !rows.length"
        class="py-8 text-center text-sm text-[var(--text-tertiary)]"
      >
        {{ t('security.auditEmpty') }}
      </p>
      <div class="mt-4 flex items-center justify-end gap-3">
        <ConsoleButton
          variant="secondary"
          :disabled="page <= 1 || loading"
          @click="page--"
          >{{ t('common.prevPage') }}</ConsoleButton
        ><span class="text-sm"
          >{{ page }} / {{ Math.max(1, Math.ceil(total / 20)) }}</span
        ><ConsoleButton
          variant="secondary"
          :disabled="page * 20 >= total || loading"
          @click="page++"
          >{{ t('common.nextPage') }}</ConsoleButton
        >
      </div>
    </ConsoleCard>
  </div>
</template>
