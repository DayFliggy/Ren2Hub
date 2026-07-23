import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/lab'
import { ApiError } from '@/api/types'
import { useToast } from '@/composables/useToast'
import type {
  AssetItem,
  ChatConversation,
  LabModelPick,
  LabStarter,
  MarketPlugin,
  McpServer,
  NoteItem,
  PluginItem,
  SkillItem,
  StudioKind,
  StudioTool,
  StudioWork,
} from '@/types/lab'

/** Conversation list rows omit the message array (loaded on open). */
export type ConversationSummary = Omit<ChatConversation, 'messages'>

interface ChatLanding {
  models: LabModelPick[]
  starters: LabStarter[]
  conversations: ConversationSummary[]
}

interface StorageInfo {
  usedBytes: number
  totalBytes: number
}

function useLoadErrorReporter() {
  const { t } = useI18n()
  const toast = useToast()
  return (error: unknown) =>
    toast.error(error instanceof ApiError ? error.message : t('common.failed'))
}

/* ---------------- chat ---------------- */
export function useLabChat() {
  const reportError = useLoadErrorReporter()
  const loading = ref(true)
  const models = ref<LabModelPick[]>([])
  const starters = ref<LabStarter[]>([])
  const conversations = ref<ConversationSummary[]>([])

  async function load() {
    loading.value = true
    try {
      const data = await api.get<ChatLanding>('/api/lab/chat/landing')
      models.value = data.models
      starters.value = data.starters
      conversations.value = data.conversations
    } catch (error) {
      reportError(error)
    } finally {
      loading.value = false
    }
  }

  return { loading, models, starters, conversations, load }
}

export function useLabConversation() {
  const reportError = useLoadErrorReporter()
  const loading = ref(true)
  const conversation = ref<ChatConversation | null>(null)

  async function load(id: string) {
    loading.value = true
    try {
      conversation.value = await api.get<ChatConversation>(
        `/api/lab/chat/conversation/${id}`
      )
    } catch (error) {
      reportError(error)
    } finally {
      loading.value = false
    }
  }

  return { loading, conversation, load }
}

/* ---------------- studio ---------------- */
export function useLabStudio() {
  const reportError = useLoadErrorReporter()
  const loading = ref(true)
  const works = ref<StudioWork[]>([])
  const tools = ref<StudioTool[]>([])

  async function load(kind?: StudioKind | 'all') {
    loading.value = true
    try {
      const data = await api.get<{ works: StudioWork[]; tools: StudioTool[] }>(
        '/api/lab/studio',
        kind && kind !== 'all' ? { kind } : undefined
      )
      works.value = data.works
      tools.value = data.tools
    } catch (error) {
      reportError(error)
    } finally {
      loading.value = false
    }
  }

  return { loading, works, tools, load }
}

/* ---------------- assets ---------------- */
export function useLabAssets() {
  const reportError = useLoadErrorReporter()
  const loading = ref(true)
  const items = ref<AssetItem[]>([])
  const storage = ref<StorageInfo | null>(null)

  async function load(kind = 'all') {
    loading.value = true
    try {
      const data = await api.get<{ items: AssetItem[]; storage: StorageInfo }>(
        '/api/lab/assets',
        {
          kind,
        }
      )
      items.value = data.items
      storage.value = data.storage
    } catch (error) {
      reportError(error)
    } finally {
      loading.value = false
    }
  }

  return { loading, items, storage, load }
}

/* ---------------- notes ---------------- */
export function useLabNotes() {
  const reportError = useLoadErrorReporter()
  const loading = ref(true)
  const items = ref<NoteItem[]>([])

  async function load() {
    loading.value = true
    try {
      const data = await api.get<{ items: NoteItem[] }>('/api/lab/notes')
      items.value = data.items
    } catch (error) {
      reportError(error)
    } finally {
      loading.value = false
    }
  }

  return { loading, items, load }
}

/* ---------------- plugins ---------------- */
export function useLabPlugins() {
  const reportError = useLoadErrorReporter()
  const loading = ref(true)
  const plugins = ref<PluginItem[]>([])
  const mcp = ref<McpServer[]>([])
  const skills = ref<SkillItem[]>([])
  const market = ref<MarketPlugin[]>([])

  async function load() {
    loading.value = true
    try {
      const data = await api.get<{
        plugins: PluginItem[]
        mcp: McpServer[]
        skills: SkillItem[]
        market: MarketPlugin[]
      }>('/api/lab/plugins')
      plugins.value = data.plugins
      mcp.value = data.mcp
      skills.value = data.skills
      market.value = data.market
    } catch (error) {
      reportError(error)
    } finally {
      loading.value = false
    }
  }

  return { loading, plugins, mcp, skills, market, load }
}
