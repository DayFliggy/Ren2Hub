<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/console'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'

interface CustomOAuthProvider {
  id: number
  name: string
  slug: string
  icon: string
  enabled: boolean
  client_id: string
  authorization_endpoint: string
  token_endpoint: string
  user_info_endpoint: string
  scopes: string
  user_id_field: string
  username_field: string
  display_name_field: string
  email_field: string
  well_known: string
  auth_style: number
  access_policy: string
  access_denied_message: string
}

interface DiscoveryResponse {
  well_known_url: string
  discovery: Record<string, unknown>
}

const { t } = useI18n()
const toast = useToast()
const providers = ref<CustomOAuthProvider[]>([])
const loading = ref(false)
const saving = ref(false)
const discovering = ref(false)
const dialogOpen = ref(false)
const deleteTarget = ref<CustomOAuthProvider | null>(null)
const editingId = ref<number | null>(null)

const form = reactive({
  name: '',
  slug: '',
  icon: '',
  enabled: true,
  client_id: '',
  client_secret: '',
  authorization_endpoint: '',
  token_endpoint: '',
  user_info_endpoint: '',
  scopes: 'openid profile email',
  user_id_field: 'sub',
  username_field: 'preferred_username',
  display_name_field: 'name',
  email_field: 'email',
  well_known: '',
  auth_style: 0,
  access_policy: '',
  access_denied_message: '',
})

function resetForm(provider?: CustomOAuthProvider) {
  editingId.value = provider?.id ?? null
  form.name = provider?.name ?? ''
  form.slug = provider?.slug ?? ''
  form.icon = provider?.icon ?? ''
  form.enabled = provider?.enabled ?? true
  form.client_id = provider?.client_id ?? ''
  form.client_secret = ''
  form.authorization_endpoint = provider?.authorization_endpoint ?? ''
  form.token_endpoint = provider?.token_endpoint ?? ''
  form.user_info_endpoint = provider?.user_info_endpoint ?? ''
  form.scopes = provider?.scopes || 'openid profile email'
  form.user_id_field = provider?.user_id_field || 'sub'
  form.username_field = provider?.username_field || 'preferred_username'
  form.display_name_field = provider?.display_name_field || 'name'
  form.email_field = provider?.email_field || 'email'
  form.well_known = provider?.well_known ?? ''
  form.auth_style = provider?.auth_style ?? 0
  form.access_policy = provider?.access_policy ?? ''
  form.access_denied_message = provider?.access_denied_message ?? ''
}

async function loadProviders() {
  loading.value = true
  try {
    const data = await api.get<CustomOAuthProvider[]>(
      '/api/custom-oauth-provider/'
    )
    providers.value = Array.isArray(data) ? data : []
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}

function openCreate() {
  resetForm()
  dialogOpen.value = true
}

function openEdit(provider: CustomOAuthProvider) {
  resetForm(provider)
  dialogOpen.value = true
}

async function discoverEndpoints() {
  if (!form.well_known.trim()) {
    toast.error(t('systemSettings.customOAuth.wellKnownRequired'))
    return
  }

  discovering.value = true
  try {
    const result = await api.post<DiscoveryResponse>(
      '/api/custom-oauth-provider/discovery',
      { well_known_url: form.well_known }
    )
    const discovery = result.discovery
    form.well_known = result.well_known_url
    form.authorization_endpoint = String(discovery.authorization_endpoint ?? '')
    form.token_endpoint = String(discovery.token_endpoint ?? '')
    form.user_info_endpoint = String(discovery.userinfo_endpoint ?? '')
    if (Array.isArray(discovery.scopes_supported)) {
      form.scopes = discovery.scopes_supported
        .filter((scope): scope is string => typeof scope === 'string')
        .join(' ')
    }
    toast.success(t('systemSettings.customOAuth.discovered'))
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    discovering.value = false
  }
}

async function saveProvider() {
  if (!form.name.trim() || !form.slug.trim() || !form.client_id.trim()) {
    toast.error(t('systemSettings.customOAuth.requiredFields'))
    return
  }
  if (
    editingId.value === null &&
    (!form.client_secret.trim() ||
      !form.authorization_endpoint.trim() ||
      !form.token_endpoint.trim() ||
      !form.user_info_endpoint.trim())
  ) {
    toast.error(t('systemSettings.customOAuth.newProviderIncomplete'))
    return
  }
  if (form.access_policy.trim()) {
    try {
      JSON.parse(form.access_policy)
    } catch {
      toast.error(t('systemSettings.customOAuth.invalidPolicy'))
      return
    }
  }

  saving.value = true
  const payload = { ...form }
  try {
    if (editingId.value === null) {
      await api.post('/api/custom-oauth-provider/', payload)
    } else {
      await api.put(`/api/custom-oauth-provider/${editingId.value}`, payload)
    }
    dialogOpen.value = false
    await loadProviders()
    toast.success(t('systemSettings.customOAuth.saved'))
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    saving.value = false
  }
}

async function deleteProvider() {
  if (!deleteTarget.value) return
  saving.value = true
  try {
    await api.delete(`/api/custom-oauth-provider/${deleteTarget.value.id}`)
    deleteTarget.value = null
    await loadProviders()
    toast.success(t('systemSettings.customOAuth.deleted'))
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    saving.value = false
  }
}

onMounted(loadProviders)
</script>

<template>
  <section
    class="custom-oauth-panel"
    :aria-label="t('systemSettings.customOAuth.title')"
  >
    <div class="custom-oauth-panel-header">
      <div>
        <h3>{{ t('systemSettings.customOAuth.title') }}</h3>
        <p>{{ t('systemSettings.customOAuth.secretNotice') }}</p>
      </div>
      <ConsoleButton size="sm" @click="openCreate">{{
        t('systemSettings.customOAuth.add')
      }}</ConsoleButton>
    </div>

    <div v-if="loading" class="py-4 text-sm text-[var(--text-tertiary)]">
      {{ t('common.loading') }}
    </div>
    <div
      v-else-if="providers.length === 0"
      class="py-4 text-sm text-[var(--text-tertiary)]"
    >
      {{ t('systemSettings.customOAuth.empty') }}
    </div>
    <div v-else class="custom-oauth-list">
      <article
        v-for="provider in providers"
        :key="provider.id"
        class="custom-oauth-row"
      >
        <div class="min-w-0">
          <p class="font-semibold text-[var(--text-primary)]">
            {{ provider.name }}
          </p>
          <p
            class="mt-0.5 truncate font-mono text-xs text-[var(--text-tertiary)]"
          >
            {{ provider.slug }}
          </p>
        </div>
        <div class="flex items-center gap-2">
          <StatusChip :tone="provider.enabled ? 'success' : 'neutral'">
            {{
              t(
                provider.enabled
                  ? 'systemSettings.customOAuth.enabled'
                  : 'systemSettings.customOAuth.disabled'
              )
            }}
          </StatusChip>
          <ConsoleButton
            variant="ghost"
            size="sm"
            @click="openEdit(provider)"
            >{{ t('common.edit') }}</ConsoleButton
          >
          <ConsoleButton
            variant="danger"
            size="sm"
            @click="deleteTarget = provider"
            >{{ t('common.delete') }}</ConsoleButton
          >
        </div>
      </article>
    </div>
  </section>

  <ConsoleModal
    :open="dialogOpen"
    :title="
      t(
        editingId === null
          ? 'systemSettings.customOAuth.createTitle'
          : 'systemSettings.customOAuth.editTitle'
      )
    "
    size="lg"
    @close="dialogOpen = false"
  >
    <div class="grid gap-4 sm:grid-cols-2">
      <FormField :label="t('systemSettings.customOAuth.name')"
        ><TextInput v-model="form.name"
      /></FormField>
      <FormField label="Slug"><TextInput v-model="form.slug" /></FormField>
      <FormField :label="t('systemSettings.customOAuth.icon')"
        ><TextInput v-model="form.icon"
      /></FormField>
      <FormField label="Client ID"
        ><TextInput v-model="form.client_id"
      /></FormField>
      <FormField label="Client Secret">
        <TextInput
          v-model="form.client_secret"
          type="password"
          autocomplete="new-password"
        />
      </FormField>
      <FormField
        :label="t('systemSettings.customOAuth.authorizationEndpoint')"
        class="sm:col-span-2"
        ><TextInput v-model="form.authorization_endpoint" type="url"
      /></FormField>
      <FormField
        :label="t('systemSettings.customOAuth.tokenEndpoint')"
        class="sm:col-span-2"
        ><TextInput v-model="form.token_endpoint" type="url"
      /></FormField>
      <FormField
        :label="t('systemSettings.customOAuth.userInfoEndpoint')"
        class="sm:col-span-2"
        ><TextInput v-model="form.user_info_endpoint" type="url"
      /></FormField>
      <FormField label="Scopes"><TextInput v-model="form.scopes" /></FormField>
      <FormField :label="t('systemSettings.customOAuth.wellKnown')">
        <div class="flex gap-2">
          <TextInput v-model="form.well_known" type="url" />
          <ConsoleButton
            variant="secondary"
            size="sm"
            :loading="discovering"
            @click="discoverEndpoints"
          >
            Discovery
          </ConsoleButton>
        </div>
      </FormField>
      <FormField :label="t('systemSettings.customOAuth.userIdField')"
        ><TextInput v-model="form.user_id_field"
      /></FormField>
      <FormField :label="t('systemSettings.customOAuth.usernameField')"
        ><TextInput v-model="form.username_field"
      /></FormField>
      <FormField :label="t('systemSettings.customOAuth.displayNameField')"
        ><TextInput v-model="form.display_name_field"
      /></FormField>
      <FormField :label="t('systemSettings.customOAuth.emailField')"
        ><TextInput v-model="form.email_field"
      /></FormField>
      <FormField :label="t('systemSettings.customOAuth.authStyle')">
        <select v-model.number="form.auth_style" class="custom-oauth-select">
          <option :value="0">
            {{ t('systemSettings.customOAuth.authStyleAuto') }}
          </option>
          <option :value="1">
            {{ t('systemSettings.customOAuth.authStyleParams') }}
          </option>
          <option :value="2">Basic Auth Header</option>
        </select>
      </FormField>
      <FormField :label="t('systemSettings.customOAuth.accessDeniedMessage')"
        ><TextInput v-model="form.access_denied_message"
      /></FormField>
      <FormField
        :label="t('systemSettings.customOAuth.accessPolicy')"
        class="sm:col-span-2"
      >
        <textarea
          v-model="form.access_policy"
          rows="4"
          class="custom-oauth-textarea"
          spellcheck="false"
        />
      </FormField>
      <div
        class="flex items-center justify-between border-t border-[var(--border-subtle)] pt-3 sm:col-span-2"
      >
        <span class="text-sm font-semibold text-[var(--text-primary)]">{{
          t('systemSettings.customOAuth.enableProvider')
        }}</span>
        <ConsoleToggle
          v-model="form.enabled"
          :label="t('systemSettings.customOAuth.enableProvider')"
        />
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <ConsoleButton
          variant="secondary"
          :disabled="saving"
          @click="dialogOpen = false"
          >{{ t('common.cancel') }}</ConsoleButton
        >
        <ConsoleButton :loading="saving" @click="saveProvider">{{
          t('systemSettings.customOAuth.save')
        }}</ConsoleButton>
      </div>
    </template>
  </ConsoleModal>

  <ConfirmDialog
    :open="deleteTarget !== null"
    :title="t('systemSettings.customOAuth.deleteTitle')"
    :message="
      t('systemSettings.customOAuth.deleteMessage', {
        name: deleteTarget?.name ?? '',
      })
    "
    :loading="saving"
    :confirm-text="t('systemSettings.customOAuth.confirmDelete')"
    @cancel="deleteTarget = null"
    @confirm="deleteProvider"
  />
</template>

<style scoped>
.custom-oauth-panel {
  margin-top: 1.5rem;
  border-top: 1px dashed var(--border-default);
  padding-top: 1rem;
}
.custom-oauth-panel-header,
.custom-oauth-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}
.custom-oauth-select,
.custom-oauth-textarea {
  width: 100%;
  border: 1.5px solid var(--border-default);
  border-radius: var(--sketch-border-radius-sm);
  background: transparent;
  color: var(--text-primary);
  font: inherit;
  outline: none;
}
.custom-oauth-select {
  height: 2.75rem;
  padding: 0 0.75rem;
}
.custom-oauth-textarea {
  min-height: 6rem;
  resize: vertical;
  padding: 0.75rem;
  font-family: var(--font-mono, monospace);
  font-size: 0.75rem;
}
.custom-oauth-panel-header h3 {
  font-size: 0.875rem;
  font-weight: 700;
  color: var(--text-primary);
}
.custom-oauth-panel-header p {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
.custom-oauth-list {
  margin-top: 0.75rem;
  border-top: 1px solid var(--border-subtle);
}
.custom-oauth-row {
  min-height: 4rem;
  border-bottom: 1px solid var(--border-subtle);
  padding: 0.75rem 0;
}
@media (max-width: 767px) {
  .custom-oauth-panel-header,
  .custom-oauth-row {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
