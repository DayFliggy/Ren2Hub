<script setup lang="ts">
import PasswordInput from '@/components/auth/PasswordInput.vue'
import PasswordStrengthMeter from '@/components/auth/PasswordStrengthMeter.vue'
import UserPermissionsEditor from './UserPermissionsEditor.vue'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useToast } from '@/composables/useToast'
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import AdminUserBindings from './AdminUserBindings.vue'
import {
  adminUserRoleChoices,
  adminUserRoleLabelKey,
  adminUserRoleTone,
  isValidAdminUsername,
  trimGoSpace,
  type AdminOperator,
} from '@/constants/adminUsers'
import type {
  AdminUser,
  AdminUserCreateInput,
  AdminUserRole,
  AdminUserUpdateInput,
} from '@/types/console'

const props = defineProps<{
  open: boolean
  editing: AdminUser | null
  /** Caps the role picker — an operator can never mint a peer or a superior. */
  operator: AdminOperator | null
  save: (input: AdminUserCreateInput | AdminUserUpdateInput) => Promise<boolean>
}>()

const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const auth = useAuthStore(),
  permissionRequest = useLatestRequest(),
  toast = useToast()
const permissions = ref<Record<string, Record<string, boolean>>>({}),
  permissionBaseline = ref(''),
  permissionsReady = ref(false)
async function loadPermissions(user: AdminUser) {
  permissionsReady.value = false
  const result = await permissionRequest.run((signal) =>
    api.get<AdminUser>(`/api/user/${user.id}`, undefined, { signal })
  )
  if (result.stale) return
  if (result.ok) {
    permissions.value = result.value.admin_permissions ?? {}
    permissionBaseline.value = JSON.stringify(permissions.value)
    permissionsReady.value = true
  } else
    toast.error(
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    )
}
const saving = ref(false)
const form = reactive({
  max_concurrency: 0,
  username: '',
  displayName: '',
  remark: '',
  password: '',
  role: '1',
})

/** Upstream validates remark with `max=255`, which counts runes. */
const REMARK_MAX_LENGTH = 255
/** Display only: upstream `EditWithTx` never writes email; bindings own it. */
const boundEmail = ref('')

/** The privilege ceiling is visible in the picker, not only enforced on submit. */
const roleChoices = computed(() =>
  adminUserRoleChoices(props.operator, props.editing?.role)
)
const roleOptions = computed(() =>
  roleChoices.value.map((role) => {
    const tone = adminUserRoleTone(role)
    return {
      value: String(role),
      label: t(adminUserRoleLabelKey(role)),
      tone: tone === 'neutral' ? undefined : tone,
    }
  })
)

const selectedRole = computed(() => Number(form.role) as AdminUserRole)
const remarkLength = computed(() => [...form.remark].length)
const valid = computed(
  () =>
    isValidAdminUsername(form.username) &&
    Number.isSafeInteger(form.max_concurrency) &&
    form.max_concurrency >= 0 &&
    remarkLength.value <= REMARK_MAX_LENGTH &&
    ((props.editing !== null && !form.password) ||
      ([...form.password].length >= 8 && [...form.password].length <= 128)) &&
    form.displayName.trim().length <= 20 &&
    roleChoices.value.includes(selectedRole.value)
)

watch(
  () => props.open,
  (open) => {
    if (!open) {
      permissionRequest.cancel()
      return
    }
    const user = props.editing
    permissionsReady.value = false
    if (auth.isRoot && user && user.role >= 10) void loadPermissions(user)
    form.max_concurrency = user?.max_concurrency ?? 0
    form.username = user?.username ?? ''
    form.displayName = user?.display_name ?? ''
    form.remark = user?.remark ?? ''
    boundEmail.value = user?.email ?? ''
    form.password = ''
    form.role = String(user?.role ?? 1)
  },
  { immediate: true }
)

function close() {
  if (!saving.value) emit('close')
}

function bindingRemoved(key: string) {
  if (key === 'email') boundEmail.value = ''
}

async function submit() {
  if (!valid.value || saving.value) return
  saving.value = true
  try {
    const base = {
      max_concurrency: form.max_concurrency,
      username: trimGoSpace(form.username),
      display_name: form.displayName.trim(),
      role: selectedRole.value,
    }
    const input: AdminUserCreateInput | AdminUserUpdateInput =
      props.editing === null
        ? { ...base, password: form.password }
        : {
            ...base,
            remark: form.remark,
            ...(form.password ? { password: form.password } : {}),
          }
    if (
      auth.isRoot &&
      props.editing?.role === 10 &&
      permissionsReady.value &&
      JSON.stringify(permissions.value) !== permissionBaseline.value
    )
      input.admin_permissions = permissions.value
    if (await props.save(input)) emit('close')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="editing ? t('users.editTitle') : t('users.createTitle')"
    size="lg"
    @close="close"
  >
    <div class="sibling-space-y-5 text-left">
      <FormField :label="t('users.username')" :hint="t('users.usernameHint')">
        <TextInput
          v-model="form.username"
          name="admin-user-username"
          :placeholder="t('users.usernamePlaceholder')"
          autocomplete="off"
          autofocus
        />
      </FormField>
      <FormField
        :label="t('users.maxConcurrency')"
        :hint="t('users.maxConcurrencyHint')"
      >
        <input
          v-model.number="form.max_concurrency"
          type="number"
          min="0"
          step="1"
          name="admin-user-max-concurrency"
          class="console-input w-full"
        />
      </FormField>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField :label="t('users.displayName')">
          <TextInput
            v-model="form.displayName"
            name="admin-user-display-name"
            :placeholder="t('users.displayNamePlaceholder')"
            autocomplete="off"
          />
        </FormField>
        <FormField
          v-if="editing !== null"
          :label="t('users.email')"
          :hint="t('users.emailManagedByBinding')"
        >
          <TextInput
            :model-value="boundEmail"
            type="email"
            name="admin-user-email"
            :placeholder="t('users.emailUnbound')"
            readonly
          />
        </FormField>
      </div>

      <FormField
        v-if="editing !== null"
        :label="t('users.remark')"
        :hint="
          t('users.remarkHint', {
            count: remarkLength,
            max: REMARK_MAX_LENGTH,
          })
        "
      >
        <textarea
          v-model="form.remark"
          rows="3"
          name="admin-user-remark"
          :aria-invalid="remarkLength > REMARK_MAX_LENGTH"
          :placeholder="t('users.remarkPlaceholder')"
          class="w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-2.5 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] transition-colors focus:border-[var(--border-strong)] focus-ring"
        />
      </FormField>

      <FormField v-if="editing === null" :label="t('users.initialPassword')">
        <PasswordInput
          v-model="form.password"
          type="password"
          name="admin-user-password"
          :placeholder="t('users.initialPasswordPlaceholder')"
          autocomplete="new-password"
        />
      </FormField>
      <PasswordStrengthMeter
        v-if="editing === null"
        :password="form.password"
      />

      <div>
        <p class="mb-1.5 text-sm font-medium text-[var(--text-secondary)]">
          {{ t('users.role') }}
        </p>
        <FilterSelect
          v-model="form.role"
          :options="roleOptions"
          :label="t('users.role')"
          class="w-full"
        />
        <p class="mt-1.5 text-xs text-[var(--text-tertiary)]">
          {{ t('users.roleCeilingHint') }}
        </p>
      </div>
    </div>

    <UserPermissionsEditor
      v-if="auth.isRoot && editing?.role === 10 && permissionsReady"
      v-model="permissions"
      class="mt-5 border-t border-[var(--border-subtle)] pt-5"
    />
    <AdminUserBindings
      v-if="open && editing"
      :key="editing.id"
      :user="editing"
      @removed="bindingRemoved"
    />
    <template #footer>
      <div class="grid grid-cols-2 gap-3">
        <ConsoleButton
          variant="secondary"
          size="lg"
          :disabled="saving"
          @click="close"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton
          size="lg"
          :loading="saving"
          :disabled="!valid"
          @click="submit"
        >
          {{ t('users.saveUser') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
