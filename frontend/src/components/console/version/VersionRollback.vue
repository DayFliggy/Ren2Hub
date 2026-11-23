<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { AlertTriangle, Check, Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { systemUpdateApi, type RollbackVersion } from '@/api/systemUpdate'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { systemUpdateMessages } from '@/i18n/systemUpdate'
import { updateErrorMessage } from './errorMessage'
import { displayVersion, publishedDate } from './format'

/**
 * The rollback list, read when the section opens. Selecting a version shows
 * the warning, the operator command that keeps a database backup, and the
 * confirm button; the rollback itself runs in the badge so closing the panel
 * does not cancel it.
 */
const props = defineProps<{ release: boolean; busy: boolean }>()
const emit = defineEmits<{ rollback: [version: string] }>()

const { t, locale } = useI18n({
  useScope: 'local',
  messages: systemUpdateMessages,
})

const versions = ref<RollbackVersion[] | null>(null)
const loading = ref(false)
const error = ref('')
const selected = ref('')
const copied = ref<'done' | 'failed' | null>(null)
const labelId = useId()
let request: AbortController | undefined

const command = computed(
  () =>
    `sudo -n python3 -B scripts/server/ren2hub-app.py rollback --version ${displayVersion(selected.value)}`
)

async function load() {
  request?.abort()
  const current = new AbortController()
  request = current
  loading.value = true
  error.value = ''
  try {
    const value = await systemUpdateApi.rollbackVersions(current.signal)
    if (!current.signal.aborted) versions.value = value
  } catch (cause) {
    if (current.signal.aborted) return
    const { key, params } = updateErrorMessage(cause, 'read')
    error.value = t(key, params ?? {})
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}

function select(version: string) {
  if (props.busy) return
  selected.value = selected.value === version ? '' : version
  copied.value = null
}

async function copy() {
  try {
    await navigator.clipboard.writeText(command.value)
    copied.value = 'done'
  } catch {
    copied.value = 'failed'
  }
}

onMounted(() => {
  if (props.release) void load()
})
onBeforeUnmount(() => request?.abort())
</script>

<template>
  <div class="version-rollback" data-version-rollback>
    <p v-if="!release" class="version-note" data-version-rollback-source>
      {{ t('rollbackSource') }}
    </p>
    <p v-else-if="loading" class="version-note" role="status">
      {{ t('rollbackLoading') }}
    </p>
    <div v-else-if="error" class="version-rollback-error" role="alert">
      <p>{{ error }}</p>
      <ConsoleButton
        size="sm"
        variant="secondary"
        data-version-rollback-retry
        @click="load"
        >{{ t('retry') }}</ConsoleButton
      >
    </div>
    <p v-else-if="versions && !versions.length" class="version-note">
      {{ t('rollbackEmpty') }}
    </p>
    <template v-else-if="versions">
      <p :id="labelId" class="version-note">
        {{ t('rollbackSelect') }}
      </p>
      <div
        role="radiogroup"
        :aria-labelledby="labelId"
        class="version-rollback-list"
      >
        <button
          v-for="item in versions"
          :key="item.version"
          type="button"
          role="radio"
          class="version-rollback-option"
          :aria-checked="selected === item.version"
          :disabled="busy"
          :data-version-rollback-option="item.version"
          @click="select(item.version)"
        >
          <span class="version-rollback-dot" aria-hidden="true" />
          <span class="version-rollback-tag">{{
            displayVersion(item.version)
          }}</span>
          <span class="version-rollback-date">{{
            publishedDate(item.published_at, locale)
          }}</span>
        </button>
      </div>
      <div v-if="selected" class="version-rollback-selected">
        <p class="version-rollback-warning">
          <AlertTriangle :size="14" aria-hidden="true" />
          <span>{{ t('rollbackWarning') }}</span>
        </p>
        <p class="version-note">{{ t('rollbackCommand') }}</p>
        <div class="version-rollback-command">
          <code data-version-rollback-command>{{ command }}</code>
          <button
            type="button"
            class="version-icon-button"
            :aria-label="t('copy')"
            :title="t('copy')"
            data-version-rollback-copy
            @click="copy"
          >
            <Check v-if="copied === 'done'" :size="15" aria-hidden="true" />
            <Copy v-else :size="15" aria-hidden="true" />
          </button>
        </div>
        <p v-if="copied" class="version-note" role="status">
          {{ copied === 'done' ? t('copied') : t('copyFailed') }}
        </p>
        <ConsoleButton
          variant="danger"
          size="sm"
          class="w-full"
          :disabled="busy"
          data-version-rollback-confirm
          @click="emit('rollback', selected)"
          >{{
            t('rollbackConfirm', { version: displayVersion(selected) })
          }}</ConsoleButton
        >
      </div>
    </template>
  </div>
</template>
