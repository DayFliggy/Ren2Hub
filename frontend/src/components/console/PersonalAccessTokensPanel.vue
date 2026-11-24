<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import SecurityProofDialog from '@/components/auth/SecurityProofDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import FormField from '@/components/common/FormField.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import {
  personalAccessTokensApi,
  scopeResource,
  type LegacyAccessToken,
  type PersonalAccessToken,
  type PATScopeCatalog,
} from '@/api/personalAccessTokens'
import { personalAccessTokenMessages } from '@/i18n/personalAccessTokens'
import { useServerText } from '@/composables/useServerText'
import { useToast } from '@/composables/useToast'
import { fromDateTimeLocal } from '@/utils/format'

const props = defineProps<{ userId: number }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: personalAccessTokenMessages,
})
const toast = useToast()
const proofDialog = ref<InstanceType<typeof SecurityProofDialog> | null>(null)
const items = ref<PersonalAccessToken[]>([])
const legacy = ref<LegacyAccessToken | null>(null)
const catalog = ref<PATScopeCatalog | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const editor = ref<'create' | 'rename' | 'scopes' | null>(null)
const selected = ref<PersonalAccessToken | null>(null)
const revokeTarget = ref<PersonalAccessToken | 'legacy' | null>(null)
const name = ref('')
const scopes = ref<string[]>([])
const expires = ref('')
const secret = ref('')
const editorFormId = useId()
let epoch = 0
let controller = new AbortController()
let disposed = false
const activeCount = computed(
  () =>
    items.value.filter(
      (item) => item.expires_at === 0 || item.expires_at > Date.now() / 1000
    ).length
)

// Catalog labels and descriptions are backend English text; zh-CN reads them
// from server-messages/permissions.json.
const serverText = useServerText()
const resourceLabels = computed(
  () =>
    new Map(
      catalog.value?.groups.flatMap((group) =>
        group.resources.map((resource) => [
          resource.resource,
          resource.label_key,
        ])
      )
    )
)
// Stored grants the catalog no longer offers stay listed by key so they can
// be cleared; saving still rejects them.
const unavailableScopes = computed(() =>
  (selected.value?.scopes ?? [])
    .filter((scope) => !catalog.value?.scopes.includes(scope))
    .sort()
)
// A token lists the catalog labels of the resources it covers; resources the
// catalog does not name are only counted, never shown by key.
function resourceBadges(item: PersonalAccessToken) {
  const resources = [...new Set(item.scopes.map(scopeResource))]
  const labels = resources.flatMap((resource) => {
    const label = resourceLabels.value.get(resource)
    return label ? [serverText(label)] : []
  })
  const hidden = resources.length - labels.length
  return hidden
    ? [...labels, t('pat.otherResources', { count: hidden })]
    : labels
}

async function reload() {
  const version = ++epoch
  controller.abort()
  controller = new AbortController()
  if (!props.userId) return
  loading.value = true
  error.value = ''
  try {
    const [list, choices] = await Promise.all([
      personalAccessTokensApi.list(controller.signal),
      personalAccessTokensApi.scopes(controller.signal),
    ])
    if (disposed || version !== epoch) return
    items.value = list.items
    legacy.value = list.legacy
    catalog.value = choices
  } catch (cause) {
    if (!disposed && version === epoch && !controller.signal.aborted)
      error.value = cause instanceof Error ? cause.message : t('pat.failed')
  } finally {
    if (version === epoch) loading.value = false
  }
}

function openEditor(
  mode: 'create' | 'rename' | 'scopes',
  item?: PersonalAccessToken
) {
  secret.value = ''
  selected.value = item ?? null
  name.value = item?.name ?? ''
  scopes.value = [...(item?.scopes ?? [])]
  expires.value = ''
  editor.value = mode
}

async function save() {
  const mode = editor.value
  if (!mode || busy.value || !catalog.value) return
  const requestedScopes = [...new Set(scopes.value)].sort()
  const expiry = expires.value
    ? fromDateTimeLocal(expires.value)
    : Math.floor(Date.now() / 1000) +
      catalog.value.default_lifetime_days * 86400
  if (
    expiry === null ||
    !name.value.trim() ||
    Array.from(name.value.trim()).length > 64 ||
    (mode !== 'rename' &&
      (!requestedScopes.length ||
        requestedScopes.some(
          (scope) => !catalog.value!.scopes.includes(scope)
        ))) ||
    (mode === 'create' && expires.value && expiry < Date.now() / 1000 + 3600)
  ) {
    toast.error(t('pat.invalid'))
    return
  }
  const version = epoch
  const id = selected.value?.id
  const inputName = name.value.trim()
  busy.value = true
  try {
    if (mode === 'create') {
      const proof = await proofDialog.value!.request({
        scope: 'access_token.generate',
        context: { scopes: requestedScopes, expires_at: expiry },
      })
      if (disposed || version !== epoch) return
      const result = await personalAccessTokensApi.create(
        { name: inputName, scopes: requestedScopes, expires_at: expiry },
        proof,
        controller.signal
      )
      if (disposed || version !== epoch) return
      items.value = [result.item, ...items.value]
      secret.value = result.token
    } else if (id) {
      let proof: string | undefined
      if (mode === 'scopes')
        proof = await proofDialog.value!.request({
          scope: 'access_token.update',
          context: { token_id: id, scopes: requestedScopes },
        })
      if (disposed || version !== epoch) return
      const result = await personalAccessTokensApi.update(
        id,
        {
          name: inputName,
          ...(mode === 'scopes' ? { scopes: requestedScopes } : {}),
        },
        proof,
        controller.signal
      )
      if (disposed || version !== epoch) return
      items.value = items.value.map((item) => (item.id === id ? result : item))
      toast.success(t('pat.saved'))
    }
    editor.value = null
  } catch (cause) {
    if (!disposed && version === epoch)
      toast.error(cause instanceof Error ? cause.message : String(cause))
  } finally {
    if (version === epoch) busy.value = false
  }
}

async function revoke() {
  const target = revokeTarget.value
  if (!target || busy.value) return
  const version = epoch
  busy.value = true
  try {
    // The legacy credential has no id; the backend binds its proof to {legacy:true}.
    const proof = await proofDialog.value!.request({
      scope: 'access_token.revoke',
      context: target === 'legacy' ? { legacy: true } : { token_id: target.id },
    })
    if (disposed || version !== epoch) return
    if (target === 'legacy')
      await personalAccessTokensApi.revokeLegacy(proof, controller.signal)
    else
      await personalAccessTokensApi.revoke(target.id, proof, controller.signal)
    if (disposed || version !== epoch) return
    revokeTarget.value = null
    busy.value = false
    toast.success(t('pat.revokedNotice'))
    await reload()
  } catch (cause) {
    if (!disposed && version === epoch)
      toast.error(cause instanceof Error ? cause.message : String(cause))
  } finally {
    if (version === epoch) busy.value = false
  }
}

function timestamp(value: number | null) {
  return value
    ? new Date(value * 1000).toLocaleString(locale.value)
    : t('pat.never')
}
function state(item: PersonalAccessToken) {
  return item.expires_at > 0 && item.expires_at <= Date.now() / 1000
    ? 'expired'
    : 'active'
}
async function copySecret() {
  try {
    await navigator.clipboard.writeText(secret.value)
    toast.success(t('pat.copied'))
  } catch {
    toast.error(t('pat.copyFailed'))
  }
}
watch(
  () => props.userId,
  () => {
    secret.value = ''
    editor.value = null
    revokeTarget.value = null
    items.value = []
    legacy.value = null
    catalog.value = null
    busy.value = false
    void reload()
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  disposed = true
  epoch++
  controller.abort()
  secret.value = ''
})
</script>

<template>
  <section
    class="no-handdrawn border-t border-[var(--border-subtle)] p-6 sm:p-7"
    data-testid="pat-panel"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 class="text-lg font-semibold text-[var(--text-primary)]">
          {{ t('pat.title') }}
        </h3>
        <p class="mt-1 text-sm text-[var(--text-secondary)]">
          {{ t('pat.description') }}
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <ConsoleButton
          variant="secondary"
          :disabled="busy"
          :loading="loading"
          @click="reload"
          >{{ t('pat.refresh') }}</ConsoleButton
        ><ConsoleButton
          :disabled="
            loading || busy || !catalog || activeCount >= catalog.limit
          "
          @click="openEditor('create')"
          >{{ t('pat.create') }}</ConsoleButton
        >
      </div>
    </div>
    <p v-if="catalog" class="mt-3 text-xs text-[var(--text-tertiary)]">
      {{ t('pat.limit', { used: activeCount, limit: catalog.limit }) }}
    </p>
    <p
      v-if="catalog && activeCount >= catalog.limit"
      class="mt-2 text-sm text-[var(--text-secondary)]"
    >
      {{ t('pat.limitReached') }}
    </p>
    <div
      v-if="legacy && !error"
      class="mt-4 rounded-lg border border-[var(--border-subtle)] p-4"
      data-testid="pat-legacy"
    >
      <div class="flex flex-wrap items-center justify-between gap-3">
        <span class="font-semibold text-[var(--text-primary)]"
          >{{ t('pat.legacy') }}
          <code v-if="legacy.token_hint" class="ml-3 text-xs"
            >…{{ legacy.token_hint }}</code
          ></span
        >
        <ConsoleButton
          variant="ghost"
          class="text-[var(--status-danger-text)] hover:bg-[var(--status-danger-soft)] hover:text-[var(--status-danger-text)]"
          :disabled="busy"
          @click="revokeTarget = 'legacy'"
          >{{ t('pat.revoke') }}</ConsoleButton
        >
      </div>
      <p class="mt-2 text-sm text-[var(--text-secondary)]">
        {{ t('pat.legacyRetire', { date: timestamp(legacy.retire_at) }) }}
      </p>
      <dl
        class="mt-2 grid gap-x-5 gap-y-1 text-xs text-[var(--text-tertiary)] sm:grid-cols-3"
      >
        <div v-if="legacy.created_at">
          <dt>{{ t('pat.created') }}</dt>
          <dd>{{ timestamp(legacy.created_at) }}</dd>
        </div>
        <div>
          <dt>{{ t('pat.lastUsed') }}</dt>
          <dd>{{ timestamp(legacy.last_used_at) }}</dd>
        </div>
      </dl>
    </div>
    <p
      v-if="error"
      role="alert"
      class="mt-3 text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <p
      v-else-if="!loading && !items.length"
      class="mt-4 text-sm text-[var(--text-tertiary)]"
    >
      {{ t('pat.empty') }}
    </p>
    <ul
      v-else
      class="mt-4 sibling-divide-y sibling-divide-[var(--border-subtle)]"
    >
      <li v-for="item in items" :key="item.id" class="min-w-0 py-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <span class="break-all font-semibold text-[var(--text-primary)]">{{
              item.name
            }}</span>
            <StatusChip
              :tone="state(item) === 'active' ? 'success' : 'neutral'"
            >
              {{ t(`pat.${state(item)}`) }}
            </StatusChip>
          </div>
          <div v-if="state(item) === 'active'" class="flex flex-wrap gap-1">
            <ConsoleButton
              variant="ghost"
              :disabled="busy"
              @click="openEditor('rename', item)"
              >{{ t('pat.rename') }}</ConsoleButton
            ><ConsoleButton
              variant="ghost"
              :disabled="busy || !catalog"
              @click="openEditor('scopes', item)"
              >{{ t('pat.permissions') }}</ConsoleButton
            ><ConsoleButton
              variant="ghost"
              class="text-[var(--status-danger-text)] hover:bg-[var(--status-danger-soft)] hover:text-[var(--status-danger-text)]"
              :disabled="busy"
              @click="revokeTarget = item"
              >{{ t('pat.revoke') }}</ConsoleButton
            >
          </div>
        </div>
        <p
          class="mt-2 flex flex-wrap gap-1 text-xs text-[var(--text-secondary)]"
          data-testid="pat-resources"
        >
          <span
            v-for="(badge, index) in resourceBadges(item)"
            :key="index"
            class="rounded border border-[var(--border-subtle)] px-1.5 py-0.5"
            >{{ badge }}</span
          >
        </p>
        <dl
          class="mt-2 grid gap-x-5 gap-y-1 text-xs text-[var(--text-tertiary)] sm:grid-cols-3"
        >
          <div>
            <dt>{{ t('pat.created') }}</dt>
            <dd>{{ timestamp(item.created_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('pat.lastUsed') }}</dt>
            <dd>{{ timestamp(item.last_used_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('pat.expiry') }}</dt>
            <dd>{{ timestamp(item.expires_at) }}</dd>
          </div>
        </dl>
      </li>
    </ul>
    <ConsoleModal
      :open="editor !== null"
      :title="editor === 'create' ? t('pat.create') : t('pat.edit')"
      :close-disabled="busy"
      @close="editor = null"
    >
      <form
        :id="editorFormId"
        class="sibling-space-y-4 text-left"
        @submit.prevent="save"
      >
        <FormField :label="t('pat.name')">
          <TextInput
            v-model="name"
            :disabled="busy"
            :aria-label="t('pat.name')"
          />
        </FormField>
        <fieldset v-if="editor !== 'rename'" :disabled="busy">
          <legend class="text-sm font-semibold">{{ t('pat.scopes') }}</legend>
          <p class="my-2 text-xs text-[var(--text-secondary)]">
            {{ t('pat.scopeHint') }}
          </p>
          <div
            class="subtle-scroll max-h-80 sibling-space-y-4 overflow-y-auto pr-1"
          >
            <section
              v-for="group in catalog?.groups ?? []"
              :key="group.group"
              class="no-handdrawn"
              :data-testid="`pat-group-${group.group}`"
            >
              <h4 class="text-xs font-semibold text-[var(--text-tertiary)]">
                {{ t(`pat.groups.${group.group}`) }}
              </h4>
              <div
                v-for="resource in group.resources"
                :key="resource.resource"
                class="mt-2"
              >
                <p class="text-sm font-medium text-[var(--text-primary)]">
                  {{ serverText(resource.label_key) }}
                </p>
                <label
                  v-for="action in resource.actions"
                  :key="action.scope"
                  class="mt-1 flex min-w-0 items-start gap-2 text-xs"
                >
                  <input
                    v-model="scopes"
                    type="checkbox"
                    :value="action.scope"
                    class="mt-0.5 accent-[var(--accent)]"
                  />
                  <span class="min-w-0"
                    >{{ serverText(action.label_key)
                    }}<span
                      v-if="action.description_key"
                      class="block text-[var(--text-secondary)]"
                      >{{ serverText(action.description_key) }}</span
                    ></span
                  >
                </label>
              </div>
            </section>
            <label
              v-for="scope in unavailableScopes"
              :key="scope"
              class="flex items-start gap-2 text-xs text-[var(--status-warning-text)]"
            >
              <input
                v-model="scopes"
                type="checkbox"
                :value="scope"
                class="mt-0.5 accent-[var(--accent)]"
              />{{ scope }} · {{ t('pat.unavailableScope') }}
            </label>
          </div>
        </fieldset>
        <FormField
          v-if="editor === 'create'"
          :label="t('pat.expiry')"
          :hint="
            t('pat.defaultExpiry', { days: catalog?.default_lifetime_days })
          "
        >
          <TextInput
            v-model="expires"
            type="datetime-local"
            step="1"
            :disabled="busy"
            :aria-label="t('pat.expiry')"
          />
        </FormField>
      </form>
      <template #footer>
        <div class="flex justify-end gap-2">
          <ConsoleButton
            variant="secondary"
            :disabled="busy"
            @click="editor = null"
            >{{ t('pat.cancel') }}</ConsoleButton
          ><ConsoleButton type="submit" :form="editorFormId" :loading="busy">{{
            t('pat.save')
          }}</ConsoleButton>
        </div>
      </template>
    </ConsoleModal>
    <ConsoleModal
      :open="Boolean(secret)"
      :title="t('pat.oneTime')"
      @close="secret = ''"
    >
      <code
        class="block select-all break-all rounded-lg bg-[var(--surface-muted)] p-4 text-sm"
        data-testid="pat-secret"
        >{{ secret }}</code
      >
      <template #footer>
        <div class="flex justify-end gap-2">
          <ConsoleButton variant="secondary" @click="copySecret">{{
            t('pat.copy')
          }}</ConsoleButton
          ><ConsoleButton @click="secret = ''">{{
            t('pat.close')
          }}</ConsoleButton>
        </div>
      </template>
    </ConsoleModal>
    <ConfirmDialog
      :open="Boolean(revokeTarget)"
      :title="t('pat.revokeTitle')"
      :message="
        t('pat.revokeMessage', {
          name:
            revokeTarget === 'legacy' ? t('pat.legacy') : revokeTarget?.name,
        })
      "
      :confirm-text="t('pat.revoke')"
      :loading="busy"
      @cancel="revokeTarget = null"
      @confirm="revoke"
    />
    <SecurityProofDialog ref="proofDialog" />
  </section>
</template>
