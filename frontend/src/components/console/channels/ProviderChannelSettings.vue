<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { channelProviderMessages } from '@/i18n/channelProviders'

defineProps<{ channelType: number; disabled: boolean }>()
const awsKeyType = defineModel<string>('awsKeyType', { required: true })
const awsApiType = defineModel<string>('awsApiType', { required: true })
const vertexKeyType = defineModel<string>('vertexKeyType', { required: true })
const vertexProjectId = defineModel<string>('vertexProjectId', {
  required: true,
})
const vertexRegion = defineModel<string>('vertexRegion', { required: true })
const { t } = useI18n({ useScope: 'local', messages: channelProviderMessages })
</script>

<template>
  <div v-if="channelType === 33 || channelType === 41" class="grid gap-4">
    <template v-if="channelType === 33">
      <FormField :label="t('awsApiType')" :hint="t('awsHint')">
        <select
          id="channel-aws-api-type"
          v-model="awsApiType"
          :aria-label="t('awsApiType')"
          :disabled="disabled"
          class="h-10 w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-sm focus-ring"
        >
          <option value="invoke_model">{{ t('invokeModel') }}</option>
          <option value="mantle">{{ t('mantle') }}</option>
        </select>
      </FormField>
      <FormField :label="t('awsKeyType')">
        <select
          id="channel-aws-key-type"
          v-model="awsKeyType"
          :aria-label="t('awsKeyType')"
          :disabled="disabled"
          class="h-10 w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-sm focus-ring"
        >
          <option value="ak_sk">{{ t('awsAccessKey') }}</option>
          <option value="api_key">{{ t('apiKey') }}</option>
        </select>
      </FormField>
      <p class="break-words text-xs text-[var(--text-tertiary)]">
        {{ t('awsCredentials', { separator: '|' }) }}
      </p>
    </template>
    <template v-else>
      <FormField :label="t('vertexKeyType')">
        <select
          id="channel-vertex-key-type"
          v-model="vertexKeyType"
          :aria-label="t('vertexKeyType')"
          :disabled="disabled"
          class="h-10 w-full rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 text-sm focus-ring"
        >
          <option value="json">{{ t('serviceAccount') }}</option>
          <option value="api_key">{{ t('apiKey') }}</option>
        </select>
      </FormField>
      <FormField
        v-if="vertexKeyType === 'api_key'"
        :label="t('projectId')"
        :hint="t('projectHint')"
      >
        <TextInput
          v-model="vertexProjectId"
          name="channel-vertex-project-id"
          :disabled="disabled"
          autocomplete="off"
        />
      </FormField>
      <FormField :label="t('region') + ' *'" :hint="t('regionHint')">
        <textarea
          v-model="vertexRegion"
          name="channel-vertex-region"
          :disabled="disabled"
          rows="3"
          class="console-input w-full resize-y"
          required
          :aria-invalid="!vertexRegion.trim()"
          :aria-describedby="
            !vertexRegion.trim() ? 'channel-vertex-region-error' : undefined
          "
          placeholder="global"
          autocomplete="off"
        ></textarea>
      </FormField>
      <p
        v-if="!vertexRegion.trim()"
        id="channel-vertex-region-error"
        role="alert"
        class="text-xs text-[var(--status-danger)]"
      >
        {{ t('regionRequired') }}
      </p>
    </template>
  </div>
</template>
