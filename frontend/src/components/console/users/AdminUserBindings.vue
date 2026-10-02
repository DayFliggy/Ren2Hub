<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  adminUserBindingsApi,
  type AdminUserBinding,
} from '@/api/adminUserBindings'
import SecurityProofDialog from '@/components/auth/SecurityProofDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useFeatureAccess } from '@/composables/useFeatureAccess'
import { adminOperatorLevel, canManageAdminUser } from '@/constants/adminUsers'
import { useAuthStore } from '@/stores/auth'
import type { AdminUser } from '@/types/console'

const proofDialog = ref<InstanceType<typeof SecurityProofDialog> | null>(null)
const props = defineProps<{ user: AdminUser }>()
const emit = defineEmits<{ removed: [key: string] }>()
const { t } = useI18n()
const auth = useAuthStore()
const { readOnly } = useFeatureAccess('admin', 'disabled')
const allowed = computed(
  () =>
    !readOnly.value &&
    auth.isAdmin &&
    canManageAdminUser(
      props.user,
      auth.user ? { id: auth.user.id, level: adminOperatorLevel(auth) } : null
    )
)
const open = ref(false)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const rows = ref<AdminUserBinding[]>([])
const pending = ref<AdminUserBinding | null>(null)
function confirmBinding(binding: AdminUserBinding | null) {
  pending.value = binding
  error.value = ''
}
let request: AbortController | null = null
let epoch = 0

async function load() {
  request?.abort()
  const current = new AbortController()
  request = current
  loading.value = true
  error.value = ''
  try {
    const result = await adminUserBindingsApi.list(
      props.user.id,
      current.signal
    )
    if (!current.signal.aborted) rows.value = result
  } catch (cause) {
    if (!current.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}
async function remove() {
  if (!allowed.value || !pending.value || busy.value) return
  const run = epoch
  const removedKey = pending.value.key
  busy.value = true
  error.value = ''
  try {
    const binding = pending.value
    const proof = await proofDialog.value!.request({
      scope: 'admin.user.change',
      context: { ids: [props.user.id], action: 'clear_binding' },
    })
    if (run !== epoch) return
    await adminUserBindingsApi.remove(binding, proof)
    if (run !== epoch) return
    emit('removed', removedKey)
    pending.value = null
    await load()
  } catch (cause) {
    if (run === epoch)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (run === epoch) busy.value = false
  }
}
watch([open, allowed, () => props.user.id], () => {
  epoch++
  request?.abort()
  pending.value = null
  rows.value = []
  error.value = ''
  busy.value = false
  if (open.value && allowed.value) void load()
  else open.value = false
})
onBeforeUnmount(() => {
  epoch++
  request?.abort()
})
</script>

<template>
  <SecurityProofDialog ref="proofDialog" />
  <ConsoleButton
    v-if="allowed"
    class="mt-4"
    variant="secondary"
    data-admin-bindings
    @click="open = true"
  >
    {{ t('users.bindingsTitle') }}
  </ConsoleButton>
  <ConsoleModal
    :open="open && allowed"
    :title="t('users.bindingsTitle')"
    :subtitle="user.username"
    :close-disabled="busy"
    @close="open = false"
  >
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <p
      v-else-if="!rows.length && !error"
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ t('users.bindingsEmpty') }}
    </p>
    <p
      v-if="error && !pending"
      role="alert"
      class="text-sm text-[var(--status-danger)]"
    >
      {{ error }}
    </p>
    <ul class="space-y-3">
      <li
        v-for="binding in rows"
        :key="binding.key"
        class="flex items-center justify-between gap-3"
      >
        <span class="min-w-0 break-words">{{
          binding.key === 'email' ? t('users.email') : binding.name
        }}</span>
        <ConsoleButton
          variant="danger"
          size="sm"
          :disabled="busy || loading"
          :aria-label="t('users.unbindProvider', { provider: binding.name })"
          @click="confirmBinding(binding)"
          >{{ t('users.unbind') }}</ConsoleButton
        >
      </li>
    </ul>
    <template #footer
      ><ConsoleButton
        variant="secondary"
        :disabled="busy || loading"
        @click="load"
        >{{ t('common.refresh') }}</ConsoleButton
      ></template
    >
  </ConsoleModal>
  <ConfirmDialog
    :open="!!pending && allowed"
    :title="t('users.unbindConfirmTitle')"
    :message="
      t('users.unbindConfirmMessage', {
        provider: pending?.name ?? '',
        user: user.username,
      })
    "
    :confirm-text="t('users.unbind')"
    :loading="busy"
    @cancel="confirmBinding(null)"
    @confirm="remove"
  >
    <p
      v-if="error"
      role="alert"
      class="mt-3 text-sm text-[var(--status-danger)]"
    >
      {{ error }}
    </p>
  </ConfirmDialog>
</template>
