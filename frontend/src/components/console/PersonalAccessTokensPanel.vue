<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import SecurityProofDialog from '@/components/auth/SecurityProofDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TextInput from '@/components/common/TextInput.vue'
import {
  personalAccessTokensApi,
  type PersonalAccessToken,
  type PATScopeCatalog,
} from '@/api/personalAccessTokens'
import { personalAccessTokenMessages } from '@/i18n/personalAccessTokens'
import { useToast } from '@/composables/useToast'

const props = defineProps<{ userId: number }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: personalAccessTokenMessages,
})
const toast = useToast()
const proofDialog = ref<InstanceType<typeof SecurityProofDialog> | null>(null)
const items = ref<PersonalAccessToken[]>([])
const catalog = ref<PATScopeCatalog | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const editor = ref<'create' | 'rename' | 'scopes' | null>(null)
const selected = ref<PersonalAccessToken | null>(null)
const revokeTarget = ref<PersonalAccessToken | null>(null)
const name = ref('')
const scopes = ref<string[]>([])
const expires = ref('')
const secret = ref('')
let epoch = 0
let controller = new AbortController()
let disposed = false
const activeCount = computed(
  () =>
    items.value.filter(
      (item) => !item.revoked_at && item.expires_at > Date.now() / 1000
    ).length
)

async function reload() {
  const version = ++epoch
  controller.abort()
  controller = new AbortController()
  if (!props.userId) return
  loading.value = true
  error.value = ''
  try {
    const [tokens, choices] = await Promise.all([
      personalAccessTokensApi.list(controller.signal),
      personalAccessTokensApi.scopes(controller.signal),
    ])
    if (disposed || version !== epoch) return
    items.value = tokens
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
    ? Math.floor(new Date(expires.value).getTime() / 1000)
    : 0
  if (
    !name.value.trim() ||
    Array.from(name.value.trim()).length > 100 ||
    (mode !== 'rename' &&
      (!requestedScopes.length ||
        requestedScopes.some(
          (scope) => !catalog.value!.scopes.includes(scope)
        ))) ||
    (mode === 'create' &&
      expires.value &&
      (!Number.isFinite(expiry) || expiry <= Date.now() / 1000))
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
      const proof = await proofDialog.value!.request({ scope: 'pat.create' })
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
          scope: 'pat.update',
          context: { id, scopes: requestedScopes },
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
    const proof = await proofDialog.value!.request({
      scope: 'pat.revoke',
      context: { id: target.id },
    })
    if (disposed || version !== epoch) return
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

function timestamp(value: number) {
  return value
    ? new Date(value * 1000).toLocaleString(locale.value)
    : t('pat.never')
}
function state(item: PersonalAccessToken) {
  return item.revoked_at
    ? 'revoked'
    : item.expires_at <= Date.now() / 1000
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
    class="border-t border-[var(--border-subtle)] p-6 sm:p-7"
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
    <p
      v-if="error"
      role="alert"
      class="mt-3 text-sm text-[var(--status-danger)]"
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
          <div class="min-w-0">
            <span class="break-all font-semibold text-[var(--text-primary)]">{{
              item.name
            }}</span
            ><span class="ml-3 text-xs text-[var(--text-secondary)]">{{
              t(`pat.${state(item)}`)
            }}</span>
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
              :disabled="busy"
              @click="revokeTarget = item"
              >{{ t('pat.revoke') }}</ConsoleButton
            >
          </div>
        </div>
        <p
          class="mt-2 break-all font-mono text-xs text-[var(--text-secondary)]"
        >
          {{ item.scopes.join(', ') }}
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
      :aria-label="editor === 'create' ? t('pat.create') : t('pat.edit')"
      :close-disabled="busy"
      @close="editor = null"
    >
      <form class="sibling-space-y-4" @submit.prevent="save">
        <h3 class="text-lg font-semibold">
          {{ editor === 'create' ? t('pat.create') : t('pat.edit') }}
        </h3>
        <label class="block text-sm"
          >{{ t('pat.name')
          }}<TextInput
            v-model="name"
            :disabled="busy"
            :aria-label="t('pat.name')"
        /></label>
        <fieldset v-if="editor !== 'rename'" :disabled="busy">
          <legend class="text-sm font-semibold">{{ t('pat.scopes') }}</legend>
          <p class="my-2 text-xs text-[var(--text-secondary)]">
            {{ t('pat.scopeHint') }}
          </p>
          <div class="grid max-h-64 gap-2 overflow-y-auto sm:grid-cols-2">
            <label
              v-for="scope in [
                ...new Set([...(catalog?.scopes ?? []), ...scopes]),
              ].sort()"
              :key="scope"
              class="flex min-w-0 items-start gap-2 text-xs"
              ><input
                v-model="scopes"
                type="checkbox"
                :value="scope"
                class="mt-0.5"
              /><span class="break-all"
                >{{ scope
                }}<span
                  v-if="!catalog?.scopes.includes(scope)"
                  class="block text-[var(--status-warning-text)]"
                  >{{ t('pat.unavailableScope') }}</span
                ></span
              ></label
            >
          </div>
        </fieldset>
        <label v-if="editor === 'create'" class="block text-sm"
          >{{ t('pat.expiry')
          }}<TextInput
            v-model="expires"
            type="datetime-local"
            step="1"
            :disabled="busy"
            :aria-label="t('pat.expiry')"
          /><span class="mt-1 block text-xs text-[var(--text-tertiary)]">{{
            t('pat.defaultExpiry', { days: catalog?.default_lifetime_days })
          }}</span></label
        >
        <div class="flex justify-end gap-2">
          <ConsoleButton
            variant="secondary"
            :disabled="busy"
            @click="editor = null"
            >{{ t('pat.cancel') }}</ConsoleButton
          ><ConsoleButton type="submit" :loading="busy">{{
            t('pat.save')
          }}</ConsoleButton>
        </div>
      </form>
    </ConsoleModal>
    <ConsoleModal
      :open="Boolean(secret)"
      :aria-label="t('pat.oneTime')"
      @close="secret = ''"
      ><div class="sibling-space-y-4">
        <h3 class="text-lg font-semibold">{{ t('pat.oneTime') }}</h3>
        <code
          class="block select-all break-all rounded-lg bg-[var(--surface-muted)] p-4 text-sm"
          data-testid="pat-secret"
          >{{ secret }}</code
        >
        <div class="flex justify-end gap-2">
          <ConsoleButton variant="secondary" @click="copySecret">{{
            t('pat.copy')
          }}</ConsoleButton
          ><ConsoleButton @click="secret = ''">{{
            t('pat.close')
          }}</ConsoleButton>
        </div>
      </div></ConsoleModal
    >
    <ConfirmDialog
      :open="Boolean(revokeTarget)"
      :title="t('pat.revokeTitle')"
      :message="t('pat.revokeMessage', { name: revokeTarget?.name })"
      :confirm-text="t('pat.revoke')"
      :loading="busy"
      @cancel="revokeTarget = null"
      @confirm="revoke"
    />
    <SecurityProofDialog ref="proofDialog" />
  </section>
</template>
