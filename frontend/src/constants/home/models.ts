// Canvas 中环绕的大模型节点。icon 指向 public/models 下的本地 SVG；
// 显示名按语言取自 home 文案 canvas.models.<id>。
export interface ModelNode {
  id: string
  icon: string
}

export const modelNodeNameKey = (id: string) => `canvas.models.${id}`

export const MODEL_NODES: ModelNode[] = [
  { id: 'claude', icon: '/models/claude-color.svg' },
  { id: 'openai', icon: '/models/openai.svg' },
  { id: 'gemini', icon: '/models/gemini-color.svg' },
  { id: 'deepseek', icon: '/models/deepseek-color.svg' },
  { id: 'qwen', icon: '/models/qwen-color.svg' },
  { id: 'grok', icon: '/models/grok.svg' },
  { id: 'kimi', icon: '/models/kimi-color.svg' },
  { id: 'zhipu', icon: '/models/zhipu-color.svg' },
  { id: 'doubao', icon: '/models/doubao-color.svg' },
  { id: 'minimax', icon: '/models/minimax-color.svg' },
  { id: 'hunyuan', icon: '/models/hunyuan-color.svg' },
  { id: 'mistral', icon: '/models/mistral-color.svg' },
]
