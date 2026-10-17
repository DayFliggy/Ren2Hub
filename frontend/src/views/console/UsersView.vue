<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import SecurityProofDialog from '@/components/auth/SecurityProofDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import UserFormModal from '@/components/console/users/UserFormModal.vue'
import UserQuotaModal from '@/components/console/users/UserQuotaModal.vue'
import { useAdminUsers } from '@/composables/useAdminUsers'
import type {
  AdminUser,
  AdminUserCreateInput,
  AdminUserUpdateInput,
} from '@/types/console'
import { formatQuota } from '@/utils/format'
import {
  adminUserRoleLabelKey,
  adminUserStatusLabelKey,
} from '@/constants/adminUsers'
const { t } = useI18n(),
  proof = ref<InstanceType<typeof SecurityProofDialog> | null>(null)
const {
  rows,
  total,
  page,
  pageSize,
  keyword,
  group,
  role,
  status,
  sortBy,
  sortOrder,
  loading,
  initialError,
  busy,
  operatorLevel,
  canManage,
  load,
  toggleStatus,
  createUser,
  updateUserDetails,
  deleteUser,
  adjustQuota,
} = useAdminUsers((operation) => proof.value!.request(operation))
const formOpen = ref(false),
  editing = ref<AdminUser | null>(null),
  quotaTarget = ref<AdminUser | null>(null),
  deleting = ref<AdminUser | null>(null)
function edit(user: AdminUser | null) {
  editing.value = user
  formOpen.value = true
}
async function save(input: AdminUserCreateInput | AdminUserUpdateInput) {
  return editing.value
    ? updateUserDetails(editing.value, input)
    : createUser(input as AdminUserCreateInput)
}
async function saveQuota(delta: string) {
  return quotaTarget.value ? adjustQuota(quotaTarget.value, delta) : false
}
async function remove() {
  if (deleting.value && (await deleteUser(deleting.value)))
    deleting.value = null
}
</script>
<template>
  <div>
    <PageBreadcrumb :crumbs="[t('users.title')]" /><ConsoleCard
      :title="t('users.title')"
    >
      <template #action
        ><ConsoleButton :disabled="busy" @click="edit(null)">{{
          t('users.createTitle')
        }}</ConsoleButton></template
      >
      <div class="mb-4 flex flex-wrap gap-3">
        <SearchInput
          v-model="keyword"
          :placeholder="t('users.searchPlaceholder')"
        /><label
          >{{ t('users.group') }}
          <input v-model="group" class="console-input" /></label
        ><select
          v-model="role"
          class="console-input"
          :aria-label="t('users.role')"
        >
          <option value="">{{ t('users.role') }}</option>
          <option
            v-for="value in [1, 10, 100]"
            :key="value"
            :value="String(value)"
          >
            {{ t(adminUserRoleLabelKey(value)) }}
          </option></select
        ><select
          v-model="status"
          class="console-input"
          :aria-label="t('users.status')"
        >
          <option value="">{{ t('users.status') }}</option>
          <option value="1">{{ t('common.enabled') }}</option>
          <option value="2">{{ t('common.disabled') }}</option></select
        ><select
          v-model="sortBy"
          class="console-input"
          :aria-label="t('common.sort')"
        >
          <option
            v-for="key in [
              'id',
              'username',
              'quota',
              'group',
              'created_at',
              'last_login_at',
            ]"
            :key="key"
            :value="key"
          >
            {{ key }}
          </option></select
        ><select v-model="sortOrder" class="console-input">
          <option value="desc">↓</option>
          <option value="asc">↑</option></select
        ><ConsoleButton variant="secondary" :disabled="loading" @click="load">{{
          t('common.refresh')
        }}</ConsoleButton>
      </div>
      <ErrorBanner v-if="initialError" :message="initialError" />
      <div class="overflow-x-auto">
        <table class="w-full min-w-[740px] text-left text-sm">
          <thead>
            <tr>
              <th class="py-3">ID</th>
              <th>{{ t('users.user') }}</th>
              <th>{{ t('users.role') }}</th>
              <th>{{ t('users.status') }}</th>
              <th>{{ t('users.group') }}</th>
              <th>{{ t('users.quota') }}</th>
              <th>{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="user in rows"
              :key="user.id"
              class="border-t border-[var(--border-subtle)]"
            >
              <td class="py-3">{{ user.id }}</td>
              <td>
                {{ user.username }}
                <p v-if="user.display_name" class="text-xs">
                  {{ user.display_name }}
                </p>
                <p class="text-xs text-[var(--text-tertiary)]">
                  {{ user.email }}
                </p>
              </td>
              <td>{{ t(adminUserRoleLabelKey(user.role)) }}</td>
              <td>{{ t(adminUserStatusLabelKey(user.status)) }}</td>
              <td>{{ user.group }}</td>
              <td>
                {{ formatQuota(user.quota) }}
                <p class="text-xs">
                  {{
                    t('users.quotaUsed', {
                      value: formatQuota(user.used_quota),
                    })
                  }}
                </p>
              </td>
              <td>
                <div v-if="canManage(user)" class="flex flex-wrap gap-2">
                  <ConsoleButton
                    variant="secondary"
                    :disabled="busy"
                    @click="edit(user)"
                    >{{ t('common.edit') }}</ConsoleButton
                  ><ConsoleButton
                    variant="secondary"
                    :disabled="busy"
                    @click="quotaTarget = user"
                    >{{ t('users.quota') }}</ConsoleButton
                  ><ConsoleButton
                    v-if="user.role < 100"
                    variant="secondary"
                    :disabled="busy"
                    @click="toggleStatus(user)"
                    >{{
                      user.status === 1
                        ? t('common.disable')
                        : t('common.enable')
                    }}</ConsoleButton
                  ><ConsoleButton
                    v-if="user.role < operatorLevel"
                    variant="danger"
                    :disabled="busy"
                    @click="deleting = user"
                    >{{ t('common.delete') }}</ConsoleButton
                  >
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <TablePagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
    /></ConsoleCard>
    <UserFormModal
      :open="formOpen"
      :editing="editing"
      :operator-level="operatorLevel"
      :save="save"
      @close="formOpen = false"
    />
    <UserQuotaModal
      :open="!!quotaTarget"
      :target="quotaTarget"
      :save="saveQuota"
      @close="quotaTarget = null"
    />
    <ConfirmDialog
      :open="!!deleting"
      :title="t('common.delete')"
      :message="deleting?.username ?? ''"
      @confirm="remove"
      @cancel="deleting = null"
    />
    <SecurityProofDialog ref="proof" />
  </div>
</template>
