<script setup lang="ts">
import {
  Activity,
  Pencil,
  Power,
  RefreshCw,
  Trash2,
  ListRestart,
  Gauge,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import IconButton from '@/components/common/IconButton.vue'
import type { AdminChannel } from '@/types/console'
import { isRetiredChannelType } from '@/constants/adminChannels'

defineProps<{
  channel: AdminChannel
  canOperate: boolean
  canWrite: boolean
  canSensitiveWrite: boolean
  busy: boolean
}>()
const emit = defineEmits<{
  test: []
  balance: []
  status: []
  edit: []
  delete: []
  sync: []
  provider: []
}>()
const { t } = useI18n()
</script>
<template>
  <div class="flex flex-wrap gap-1">
    <template v-if="!isRetiredChannelType(channel.type)">
      <IconButton
        v-if="canOperate"
        :label="t('channels.testChannel')"
        :disabled="busy"
        @click="emit('test')"
        ><Activity :size="16"
      /></IconButton>
      <IconButton
        v-if="
          canOperate &&
          channel.type !== 61 &&
          !channel.channel_info?.is_multi_key
        "
        :label="t('channels.refreshBalance')"
        :disabled="busy"
        @click="emit('balance')"
        ><RefreshCw :size="16"
      /></IconButton>
      <IconButton
        v-if="canOperate"
        :label="t('channels.syncModelsTitle')"
        :disabled="busy"
        @click="emit('sync')"
        ><ListRestart :size="16"
      /></IconButton>
      <IconButton
        v-if="[62, 63].includes(channel.type)"
        :label="t('channels.providerStatus')"
        :disabled="busy"
        @click="emit('provider')"
        ><Gauge :size="16"
      /></IconButton>
      <IconButton
        v-if="canWrite"
        :label="t('channels.editChannel')"
        :disabled="busy"
        @click="emit('edit')"
        ><Pencil :size="16"
      /></IconButton>
      <IconButton
        v-if="canOperate"
        :label="
          channel.status === 1
            ? t('channels.disableChannel')
            : t('channels.enableChannel')
        "
        :disabled="busy"
        @click="emit('status')"
        ><Power :size="16"
      /></IconButton>
    </template>
    <IconButton
      v-if="canSensitiveWrite"
      :label="t('channels.deleteChannel')"
      tone="danger"
      :disabled="busy"
      @click="emit('delete')"
      ><Trash2 :size="16"
    /></IconButton>
  </div>
</template>
