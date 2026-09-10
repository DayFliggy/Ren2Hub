<script setup lang="ts">
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  accountClassesApi,
  MAX_USER_CONCURRENCY,
  MAX_USER_RPM,
  parseLimitOverride,
  type AccountClass,
} from '@/api/accountClasses'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FormField from '@/components/common/FormField.vue'
import IconButton from '@/components/common/IconButton.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'

const { t } = useI18n()
const auth = useAuthStore()
const toast = useToast()
const rows = ref<AccountClass[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const editOpen = ref(false)
const editing = ref<AccountClass | null>(null)
const deleting = ref<AccountClass | null>(null)
const form = reactive({
  code: '',
  name: '',
  concurrency: '5',
  rpm: '0',
  multiplier: '1',
})
let controller: AbortController | null = null

const concurrency = computed(() =>
  parseLimitOverride(form.concurrency, MAX_USER_CONCURRENCY)
)
const rpm = computed(() => parseLimitOverride(form.rpm, MAX_USER_RPM))
const multiplier = computed(() => Number(form.multiplier))
const valid = computed(
  () =>
    form.code.length > 0 &&
    form.code.length <= 64 &&
    form.code.trim() === form.code &&
    !Array.from(form.code).some(
      (char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127
    ) &&
    form.name.trim().length > 0 &&
    form.name.length <= 128 &&
    concurrency.value != null &&
    rpm.value != null &&
    Number.isFinite(multiplier.value) &&
    multiplier.value > 0
)

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = ''
  try {
    const result = await accountClassesApi.list(request.signal)
    if (!request.signal.aborted) rows.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

function openEditor(row: AccountClass | null) {
  if (!auth.isRoot || busy.value) return
  editing.value = row
  Object.assign(form, {
    code: row?.code ?? '',
    name: row?.name ?? '',
    concurrency: String(row?.default_concurrency ?? 5),
    rpm: String(row?.default_rpm ?? 0),
    multiplier: String(row?.topup_multiplier ?? 1),
  })
  editOpen.value = true
}

async function save() {
  if (!auth.isRoot || busy.value || !valid.value) return
  busy.value = true
  const input = {
    code: form.code,
    name: form.name.trim(),
    default_concurrency: concurrency.value!,
    default_rpm: rpm.value!,
    topup_multiplier: multiplier.value,
  }
  try {
    if (editing.value)
      await accountClassesApi.update({
        ...input,
        policy_version: editing.value.policy_version,
      })
    else await accountClassesApi.create(input)
    editOpen.value = false
    toast.success(t('systemSettings.saved'))
    await load()
  } catch (cause) {
    toast.error(cause instanceof Error ? cause.message : String(cause))
  } finally {
    busy.value = false
  }
}

async function remove() {
  const row = deleting.value
  if (!auth.isRoot || !row || row.code === 'default' || busy.value) return
  busy.value = true
  try {
    await accountClassesApi.remove(row.code)
    deleting.value = null
    await load()
  } catch (cause) {
    toast.error(cause instanceof Error ? cause.message : String(cause))
  } finally {
    busy.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => controller?.abort())
</script>

<template>
  <section
    class="min-w-0 border-t border-[var(--border-subtle)] pt-6"
    :aria-label="t('accountClasses.title')"
    data-account-classes
  >
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-base font-semibold text-[var(--text-primary)]">
        {{ t('accountClasses.title') }}
      </h2>
      <div class="flex items-center gap-2">
        <IconButton
          :label="t('users.refreshList')"
          :disabled="busy || loading"
          @click="load"
          ><RefreshCw :size="16" :class="{ 'animate-spin': loading }"
        /></IconButton>
        <ConsoleButton
          v-if="auth.isRoot"
          size="sm"
          :disabled="busy || loading"
          @click="openEditor(null)"
          ><Plus :size="16" />{{ t('accountClasses.create') }}</ConsoleButton
        >
      </div>
    </div>
    <p
      v-if="error"
      role="alert"
      class="mb-3 text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <p
      v-if="!loading && !error && !rows.length"
      class="py-6 text-sm text-[var(--text-tertiary)]"
    >
      {{ t('accountClasses.empty') }}
    </p>
    <div v-if="rows.length" class="overflow-x-auto" :aria-busy="loading">
      <table class="w-full min-w-[640px] text-left text-sm">
        <thead
          class="border-b border-[var(--border-subtle)] text-xs text-[var(--text-secondary)]"
        >
          <tr>
            <th class="px-3 py-3">{{ t('accountClasses.accountClass') }}</th>
            <th class="px-3 py-3">
              {{ t('accountClasses.defaultConcurrency') }}
            </th>
            <th class="px-3 py-3">{{ t('accountClasses.defaultRPM') }}</th>
            <th class="px-3 py-3">{{ t('accountClasses.topupMultiplier') }}</th>
            <th v-if="auth.isRoot" class="px-3 py-3 text-right">
              {{ t('users.actions') }}
            </th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[var(--border-subtle)]">
          <tr v-for="row in rows" :key="row.code">
            <td class="max-w-56 break-words px-3 py-3">
              <p class="font-medium text-[var(--text-primary)]">
                {{ row.name }}
              </p>
              <p class="text-xs text-[var(--text-tertiary)]">{{ row.code }}</p>
            </td>
            <td class="px-3 py-3 tabular-nums">
              {{ row.default_concurrency || t('accountClasses.unlimited') }}
            </td>
            <td class="px-3 py-3 tabular-nums">
              {{ row.default_rpm || t('accountClasses.unlimited') }}
            </td>
            <td class="px-3 py-3 tabular-nums">{{ row.topup_multiplier }}</td>
            <td v-if="auth.isRoot" class="px-3 py-3">
              <div class="flex justify-end gap-1">
                <IconButton
                  :label="t('accountClasses.edit')"
                  :disabled="busy || loading"
                  @click="openEditor(row)"
                  ><Pencil :size="16" /></IconButton
                ><IconButton
                  :label="t('accountClasses.remove')"
                  tone="danger"
                  :disabled="busy || loading || row.code === 'default'"
                  @click="deleting = row"
                  ><Trash2 :size="16"
                /></IconButton>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <ConsoleModal
      :open="editOpen"
      :title="t(editing ? 'accountClasses.edit' : 'accountClasses.create')"
      size="lg"
      @close="!busy && (editOpen = false)"
    >
      <form class="space-y-4" @submit.prevent="save">
        <div class="grid gap-4 sm:grid-cols-2">
          <FormField :label="t('accountClasses.code')"
            ><TextInput
              v-model="form.code"
              name="account-class-code"
              :readonly="editing !== null"
              maxlength="64"
          /></FormField>
          <FormField :label="t('accountClasses.name')"
            ><TextInput
              v-model="form.name"
              name="account-class-name"
              maxlength="128"
          /></FormField>
          <FormField :label="t('accountClasses.defaultConcurrency')"
            ><TextInput
              v-model="form.concurrency"
              name="account-class-concurrency"
              type="number"
              min="0"
              :max="MAX_USER_CONCURRENCY"
              step="1"
          /></FormField>
          <FormField :label="t('accountClasses.defaultRPM')"
            ><TextInput
              v-model="form.rpm"
              name="account-class-rpm"
              type="number"
              min="0"
              :max="MAX_USER_RPM"
              step="1"
          /></FormField>
          <FormField :label="t('accountClasses.topupMultiplier')"
            ><TextInput
              v-model="form.multiplier"
              name="account-class-multiplier"
              type="number"
              min="0"
              step="any"
          /></FormField>
        </div>
        <p v-if="editing" class="text-xs text-[var(--text-tertiary)]">
          {{
            t('accountClasses.policyVersion', {
              version: editing.policy_version,
            })
          }}
        </p>
        <p v-if="!valid" class="text-sm text-[var(--text-tertiary)]">
          {{ t('accountClasses.invalid') }}
        </p>
      </form>
      <template #footer
        ><div class="flex justify-end gap-3">
          <ConsoleButton
            variant="secondary"
            :disabled="busy"
            @click="editOpen = false"
            >{{ t('common.cancel') }}</ConsoleButton
          ><ConsoleButton :disabled="!valid" :loading="busy" @click="save">{{
            t('common.save')
          }}</ConsoleButton>
        </div></template
      >
    </ConsoleModal>
    <ConfirmDialog
      :open="deleting !== null"
      :title="t('accountClasses.remove')"
      :message="
        t('accountClasses.deleteMessage', { name: deleting?.name ?? '' })
      "
      :confirm-text="t('accountClasses.remove')"
      :loading="busy"
      @confirm="remove"
      @cancel="!busy && (deleting = null)"
    />
  </section>
</template>
