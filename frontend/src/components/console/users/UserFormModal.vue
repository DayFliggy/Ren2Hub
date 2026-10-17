<script setup lang="ts">
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
  ADMIN_USER_ASSIGNABLE_ROLES,
  adminUserRoleLabelKey,
  adminUserRoleTone,
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
  operatorLevel: number
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
  username: '',
  displayName: '',
  email: '',
  password: '',
  role: '1',
  group: 'default',
})

const USERNAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]{2,19}$/
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

/**
 * Only roles strictly below the operator's own level are offered, so the
 * privilege ceiling is visible in the UI rather than only enforced on submit.
 */
const roleOptions = computed(() =>
  [
    ...ADMIN_USER_ASSIGNABLE_ROLES,
    ...(props.editing?.role === 100 ? [100 as const] : []),
  ]
    .filter(
      (role) => role < props.operatorLevel || role === props.editing?.role
    )
    .map((role) => {
      const tone = adminUserRoleTone(role)
      return {
        value: String(role),
        label: t(adminUserRoleLabelKey(role)),
        tone: tone === 'neutral' ? undefined : tone,
      }
    })
)

const selectedRole = computed(() => Number(form.role) as AdminUserRole)
const emailValid = computed(
  () => form.email.trim() === '' || EMAIL_PATTERN.test(form.email.trim())
)
const usernameValid = computed(() =>
  USERNAME_PATTERN.test(form.username.trim())
)
const valid = computed(
  () =>
    usernameValid.value &&
    (props.editing === null ||
      (emailValid.value && form.email.trim().length <= 50)) &&
    ((props.editing !== null && !form.password) ||
      ([...form.password].length >= 8 && [...form.password].length <= 128)) &&
    form.displayName.trim().length <= 20 &&
    ((ADMIN_USER_ASSIGNABLE_ROLES.includes(selectedRole.value) &&
      selectedRole.value < props.operatorLevel) ||
      (props.editing?.role === 100 &&
        selectedRole.value === 100 &&
        auth.isRoot))
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
    form.username = user?.username ?? ''
    form.displayName = user?.display_name ?? ''
    form.email = user?.email ?? ''
    form.password = ''
    form.role = String(user?.role ?? 1)
    form.group = user?.group ?? 'default'
  },
  { immediate: true }
)

function close() {
  if (!saving.value) emit('close')
}

function bindingRemoved(key: string) {
  if (key === 'email' && form.email === props.editing?.email) form.email = ''
}

async function submit() {
  if (!valid.value || saving.value) return
  saving.value = true
  try {
    const base = {
      username: form.username.trim(),
      display_name: form.displayName.trim(),
      role: selectedRole.value,
    }
    const input: AdminUserCreateInput | AdminUserUpdateInput =
      props.editing === null
        ? { ...base, password: form.password }
        : {
            ...base,
            email: form.email.trim(),
            group: form.group,
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
      <UserPermissionsEditor
        v-if="auth.isRoot && editing?.role === 10 && permissionsReady"
        v-model="permissions"
      />
      <FormField :label="t('users.username')" :hint="t('users.usernameHint')">
        <TextInput
          v-model="form.username"
          name="admin-user-username"
          :placeholder="t('users.usernamePlaceholder')"
          autocomplete="off"
        />
      </FormField>

      <FormField v-if="editing !== null" :label="t('users.group')"
        ><TextInput v-model="form.group" name="admin-user-group"
      /></FormField>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField :label="t('users.displayName')">
          <TextInput
            v-model="form.displayName"
            name="admin-user-display-name"
            :placeholder="t('users.displayNamePlaceholder')"
            autocomplete="off"
          />
        </FormField>
        <FormField v-if="editing !== null" :label="t('users.email')">
          <TextInput
            v-model="form.email"
            type="email"
            name="admin-user-email"
            :placeholder="t('users.emailPlaceholder')"
            autocomplete="off"
          />
        </FormField>
      </div>

      <FormField v-if="editing === null" :label="t('users.initialPassword')">
        <TextInput
          v-model="form.password"
          type="password"
          name="admin-user-password"
          :placeholder="t('users.initialPasswordPlaceholder')"
          autocomplete="new-password"
        />
      </FormField>

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
