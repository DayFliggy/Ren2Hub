<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import {
  accountClassesApi,
  MAX_USER_CONCURRENCY,
  MAX_USER_RPM,
  parseLimitOverride,
  type AccountClass,
} from '@/api/accountClasses'
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
const saving = ref(false)
const classes = ref<AccountClass[]>([])
const classesLoading = ref(false)
const classesError = ref('')
const classesRevision = ref(0)
const form = reactive({
  username: '',
  displayName: '',
  email: '',
  password: '',
  role: '1',
  status: 1 as 1 | 2,
  accountClass: 'default',
  concurrency: '',
  rpm: '',
})

const classOptions = computed(() =>
  classes.value.map((item) => ({
    value: item.code,
    label: `${item.name} (${item.code})`,
  }))
)
const selectedClass = computed(() =>
  classes.value.find((item) => item.code === form.accountClass)
)
const concurrencyOverride = computed(() =>
  parseLimitOverride(form.concurrency, MAX_USER_CONCURRENCY)
)
const rpmOverride = computed(() => parseLimitOverride(form.rpm, MAX_USER_RPM))

const USERNAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]{2,19}$/
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

/**
 * Only roles strictly below the operator's own level are offered, so the
 * privilege ceiling is visible in the UI rather than only enforced on submit.
 */
const roleOptions = computed(() =>
  ADMIN_USER_ASSIGNABLE_ROLES.filter((role) => role < props.operatorLevel).map(
    (role) => {
      const tone = adminUserRoleTone(role)
      return {
        value: String(role),
        label: t(adminUserRoleLabelKey(role)),
        tone: tone === 'neutral' ? undefined : tone,
      }
    }
  )
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
    !classesLoading.value &&
    selectedClass.value !== undefined &&
    concurrencyOverride.value !== undefined &&
    rpmOverride.value !== undefined &&
    usernameValid.value &&
    emailValid.value &&
    (props.editing !== null ||
      (form.password.length >= 8 && form.password.length <= 20)) &&
    form.displayName.trim().length <= 20 &&
    form.email.trim().length <= 50 &&
    ADMIN_USER_ASSIGNABLE_ROLES.includes(selectedRole.value) &&
    selectedRole.value < props.operatorLevel
)

watch(
  () => [props.open, classesRevision.value] as const,
  async ([open], previous, onCleanup) => {
    if (!open) return
    const controller = new AbortController()
    onCleanup(() => controller.abort())
    if (previous?.[0] !== true) {
      const user = props.editing
      form.username = user?.username ?? ''
      form.displayName = user?.display_name ?? ''
      form.email = user?.email ?? ''
      form.password = ''
      form.role = String(user?.role ?? 1)
      form.status = user?.status === 2 ? 2 : 1
      form.accountClass = user?.account_class ?? 'default'
      form.concurrency =
        user?.concurrency_override == null
          ? ''
          : String(user.concurrency_override)
      form.rpm = user?.rpm_override == null ? '' : String(user.rpm_override)
    }
    classesLoading.value = true
    classesError.value = ''
    try {
      const result = await accountClassesApi.list(controller.signal)
      if (!controller.signal.aborted) classes.value = result
    } catch (error) {
      if (!controller.signal.aborted) {
        classes.value = []
        classesError.value =
          error instanceof Error ? error.message : String(error)
      }
    } finally {
      if (!controller.signal.aborted) classesLoading.value = false
    }
  },
  { immediate: true }
)

function close() {
  if (!saving.value) emit('close')
}

async function submit() {
  if (!valid.value || saving.value) return
  saving.value = true
  try {
    const base: AdminUserUpdateInput = {
      username: form.username.trim(),
      display_name: form.displayName.trim(),
      email: form.email.trim(),
      role: selectedRole.value,
      account_class: form.accountClass,
      concurrency_override: concurrencyOverride.value!,
      rpm_override: rpmOverride.value!,
      policy_version: props.editing?.policy_version ?? 1,
    }
    const input: AdminUserCreateInput | AdminUserUpdateInput =
      props.editing === null
        ? { ...base, password: form.password, status: form.status }
        : base
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
    <div class="space-y-5 text-left">
      <FormField :label="t('users.username')" :hint="t('users.usernameHint')">
        <TextInput
          v-model="form.username"
          name="admin-user-username"
          :placeholder="t('users.usernamePlaceholder')"
          autocomplete="off"
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
        <FormField :label="t('users.email')">
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

      <FormField :label="t('accountClasses.accountClass')">
        <FilterSelect
          v-model="form.accountClass"
          :options="classOptions"
          :label="t('accountClasses.accountClass')"
          :disabled="classesLoading || saving"
          class="w-full"
        />
        <p
          v-if="classesError"
          role="alert"
          class="mt-2 text-sm text-[var(--status-danger-text)]"
        >
          {{ classesError }}
        </p>
        <ConsoleButton
          v-if="classesError"
          variant="secondary"
          size="sm"
          @click="classesRevision++"
          >{{ t('users.retry') }}</ConsoleButton
        >
      </FormField>
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField :label="t('accountClasses.concurrencyOverride')">
          <TextInput
            v-model="form.concurrency"
            name="admin-user-concurrency"
            type="number"
            min="0"
            :max="MAX_USER_CONCURRENCY"
            step="1"
            :placeholder="t('accountClasses.inherit')"
            :aria-invalid="concurrencyOverride === undefined"
          />
          <p class="mt-1.5 text-xs text-[var(--text-tertiary)]">
            {{
              t('accountClasses.effective', {
                value:
                  (concurrencyOverride ??
                    selectedClass?.default_concurrency) === 0
                    ? t('accountClasses.unlimited')
                    : (concurrencyOverride ??
                      selectedClass?.default_concurrency ??
                      '-'),
              })
            }}
          </p>
        </FormField>
        <FormField :label="t('accountClasses.rpmOverride')">
          <TextInput
            v-model="form.rpm"
            name="admin-user-rpm"
            type="number"
            min="0"
            :max="MAX_USER_RPM"
            step="1"
            :placeholder="t('accountClasses.inherit')"
            :aria-invalid="rpmOverride === undefined"
          />
          <p class="mt-1.5 text-xs text-[var(--text-tertiary)]">
            {{
              t('accountClasses.effective', {
                value:
                  (rpmOverride ?? selectedClass?.default_rpm) === 0
                    ? t('accountClasses.unlimited')
                    : (rpmOverride ?? selectedClass?.default_rpm ?? '-'),
              })
            }}
          </p>
        </FormField>
      </div>
      <p v-if="editing" class="text-xs text-[var(--text-tertiary)]">
        {{
          t('accountClasses.policyVersion', { version: editing.policy_version })
        }}
      </p>

      <div v-if="editing === null">
        <p class="mb-1.5 text-sm font-medium text-[var(--text-secondary)]">
          {{ t('users.status') }}
        </p>
        <div
          class="sketch-md grid grid-cols-2 gap-1 bg-[var(--surface-muted)] p-1"
          role="radiogroup"
          :aria-label="t('users.status')"
        >
          <button
            v-for="option in [
              { value: 1 as const, label: t('users.statusEnabled') },
              { value: 2 as const, label: t('users.statusDisabled') },
            ]"
            :key="option.value"
            type="button"
            role="radio"
            :aria-checked="form.status === option.value"
            class="sketch-sm h-9 text-sm font-medium transition-colors focus-ring"
            :class="
              form.status === option.value
                ? 'bg-[var(--surface-solid)] text-[var(--text-primary)] shadow-sm'
                : 'text-[var(--text-tertiary)] hover:text-[var(--text-secondary)]'
            "
            @click="form.status = option.value"
          >
            {{ option.label }}
          </button>
        </div>
      </div>
    </div>

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
