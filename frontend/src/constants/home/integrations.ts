// 首屏「已适配工具」跑马灯。icon 指向 public/integrations 下的本地图标（SVG/PNG）。
// 工具集对齐 ergouapi.com「已适配工具」清单。高亮/未高亮由 IntegrationChip 用 CSS 滤镜切换（同一图标文件）。
export interface Integration {
  id: string
  name: string
  icon: string
  /** mark：透明底单色/少色标志，未高亮时转为随主题的剪影（灰化后会融进同色底）；
   *  plate：自带底板的图标，未高亮时只去色。 */
  tone: 'mark' | 'plate'
}

export const INTEGRATIONS: Integration[] = [
  {
    id: 'claude-code',
    name: 'Claude Code',
    icon: '/integrations/claude-code.svg',
    tone: 'mark',
  },
  {
    id: 'codex',
    name: 'Codex',
    icon: '/integrations/codex.svg',
    tone: 'plate',
  },
  {
    id: 'hermes-agent',
    name: 'Hermes Agent',
    icon: '/integrations/hermes-agent.svg',
    tone: 'mark',
  },
  {
    id: 'trae',
    name: 'Trae',
    icon: '/integrations/trae-color.svg',
    tone: 'mark',
  },
  {
    id: 'cc-switch',
    name: 'CC Switch',
    icon: '/integrations/cc-switch.png',
    tone: 'mark',
  },
  {
    id: 'claude-code-router',
    name: 'Claude Code Router',
    icon: '/integrations/claude-code-router.png',
    tone: 'mark',
  },
  {
    id: 'cc-gui',
    name: 'CC GUI',
    icon: '/integrations/cc-gui.png',
    tone: 'plate',
  },
  {
    id: 'cline',
    name: 'Cline',
    icon: '/integrations/cline.png',
    tone: 'plate',
  },
  {
    id: 'continue',
    name: 'Continue',
    icon: '/integrations/continue.png',
    tone: 'plate',
  },
  {
    id: 'roo-code',
    name: 'Roo Code',
    icon: '/integrations/roo-code.png',
    tone: 'mark',
  },
  {
    id: 'kilo-code',
    name: 'Kilo Code',
    icon: '/integrations/kilo-code.png',
    tone: 'plate',
  },
  {
    id: 'aider',
    name: 'Aider',
    icon: '/integrations/aider.png',
    tone: 'plate',
  },
]
