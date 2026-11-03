<script setup lang="ts">
import { onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import {
  clearAffiliateAttribution,
  getAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'

const props = defineProps<{ disabled?: boolean; error?: string }>()
const code = defineModel<string>({ required: true })
const confirmed = defineModel<boolean>('confirmed', { default: false })
const emit = defineEmits<{ changed: [] }>()
const route = useRoute()
const router = useRouter()
const { t } = useI18n()

function update(value: string) {
  code.value = value
  confirmed.value = Boolean(value.trim())
  if (confirmed.value) storeAffiliateAttribution(value)
  else clearAffiliateAttribution()
  emit('changed')
}

watch(confirmed, (value) => {
  if (value && code.value.trim()) storeAffiliateAttribution(code.value)
  else clearAffiliateAttribution()
})

function syncAttribution(value: unknown) {
  confirmed.value = false
  if (typeof value === 'string') {
    code.value = value.trim()
    clearAffiliateAttribution()
  } else code.value = getAffiliateAttribution()
}

// Initialize after the parent has mounted so its v-model listener receives
// the restored attribution instead of being overwritten by the initial prop.
onMounted(() => syncAttribution(route.query.aff))
// An explicitly empty aff query means no attribution. Only an absent query
// falls back to the stored code, so clearing cannot resurrect a stale invite.
watch(() => route.query.aff, syncAttribution)

async function clear() {
  if (props.disabled) return
  update('')
  const query = { ...route.query }
  delete query.aff
  if (typeof route.query.aff === 'string') await router.replace({ query })
}
</script>

<template>
  <div class="mt-5 sibling-space-y-2" data-affiliate-attribution>
    <FormField
      :label="t('auth.inviteCode')"
      :hint="t('auth.inviteAttributionHint')"
    >
      <TextInput
        :model-value="code"
        :disabled="disabled"
        :placeholder="t('auth.inviteCodePlaceholder')"
        autocomplete="off"
        @update:model-value="update"
      />
    </FormField>
    <label
      v-if="code.trim()"
      class="flex items-start gap-2 text-sm text-[var(--text-secondary)]"
    >
      <input
        v-model="confirmed"
        type="checkbox"
        class="mt-1 h-4 w-4 accent-[var(--accent)]"
        :disabled="disabled"
      />
      <span>{{ t('auth.confirmInviteCode') }}</span>
    </label>
    <p
      v-if="error"
      role="alert"
      class="text-sm text-[var(--status-danger-text)]"
    >
      {{ error }} {{ t('auth.inviteRecoveryHint') }}
    </p>
    <ConsoleButton
      v-if="code.trim()"
      type="button"
      size="sm"
      variant="secondary"
      :disabled="disabled"
      @click="clear"
      >{{ t('auth.clearInviteCode') }}</ConsoleButton
    >
  </div>
</template>
