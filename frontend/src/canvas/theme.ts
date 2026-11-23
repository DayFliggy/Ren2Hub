import { normalizeOpaqueColor } from '@/utils/cssColor'

/**
 * Palette used by the canvas scene. Both themes resolve their structural
 * colours from the same CSS tokens as the DOM and fall back to the values
 * below; vendor overrides remain explicit brand-colour exceptions.
 */
export type CanvasThemeName = 'light' | 'dark'

export interface CanvasTheme {
  name: CanvasThemeName
  backgroundTop: string
  backgroundBottom: string
  mapLand: string
  mapHighlight: string
  mapOcean: string
  mapLabel: string
  mapFlow: string
  mapRipple: string
  mapGlyph: string
  mapGlyphAccent: string
  mapCursorGlow: string
  mapCursorRing: string
  modelFallback: string
  modelColorOverrides: Readonly<Record<string, string>>
  charRain: string
  charRainLead: string
  nodeSurface: string
  nodeLabelSurface: string
  nodeLabelText: string
  userAccent: string
  userCore: string
  hubHalo: string
  hubSurface: string
  hubOrbitPrimary: string
  hubOrbitSecondary: string
  hubOrbitCore: string
  hubRing: string
  hubLabel: string
  channelStroke: string
  channelGlow: string
  channelPacket: string
  channelCore: string
  responsePacket: string
  packetCore: string
  accent: string
  glowComposite: GlobalCompositeOperation
}

const CANVAS_THEMES: Record<CanvasThemeName, CanvasTheme> = {
  // B1 Studio：曜石炭蓝为底，运行时从同一组夜间语义令牌解析。
  // 薄荷绿点缀（--glow #8EC8AA）：用户/响应包=薄荷回程、枢纽副轨金绿交替、
  // 字符雨首字符薄荷、涟漪=绿金波光；哑蓝为信号/地图主调。
  dark: {
    name: 'dark',
    backgroundTop: '#161920',
    backgroundBottom: '#1c1f26',
    mapLand: '#6a8cb8',
    mapHighlight: '#e4c06b',
    mapOcean: '#1a2236',
    mapLabel: '#98a4c0',
    mapFlow: '#6db897',
    mapRipple: '#8ec8aa',
    mapGlyph: '#a8c4e8',
    mapGlyphAccent: '#e4c06b',
    mapCursorGlow: '#e4c06b',
    mapCursorRing: '#efd27e',
    modelFallback: '#6a8cb8',
    modelColorOverrides: {},
    charRain: '#364f72',
    charRainLead: '#8ec8aa',
    nodeSurface: '#262a34',
    nodeLabelSurface: '#161920',
    nodeLabelText: '#eef1f7',
    userAccent: '#8ec8aa',
    userCore: '#b8e8d0',
    hubHalo: '#364f72',
    hubSurface: '#161920',
    hubOrbitPrimary: '#e4c06b',
    hubOrbitSecondary: '#8ec8aa',
    hubOrbitCore: '#f5e49c',
    hubRing: '#e4c06b',
    hubLabel: '#eef1f7',
    channelStroke: '#e4c06b',
    channelGlow: '#e4c06b',
    channelPacket: '#f5e49c',
    channelCore: '#fffae8',
    responsePacket: '#8ec8aa',
    packetCore: '#ffffff',
    accent: '#e4c06b',
    glowComposite: 'lighter',
  },
  // Desert Ledger：暖米白纸面为底，运行时从日间令牌解析，取值与下列回退色相同。
  // 点阵=橄榄墨点，航线/数据包=焦糖批注。
  // 草甸绿点缀（田园航拍：草甸中绿/树篱深绿/麦茬金）：用户=田间人影、
  // 响应包=牧绿回程、枢纽副轨焦糖绿交替、字符雨首字符新绿、涟漪=草浪、流尾=青绿水痕。
  light: {
    name: 'light',
    backgroundTop: '#F6F3EB',
    backgroundBottom: '#ECE6D4',
    mapLand: '#74765A',
    mapHighlight: '#D8984C',
    mapOcean: '#C2BCA6',
    mapLabel: '#827E66',
    mapFlow: '#6FA894',
    mapRipple: '#7FA463',
    mapGlyph: '#5C5E45',
    mapGlyphAccent: '#A87643',
    mapCursorGlow: '#D8984C',
    mapCursorRing: '#C08B57',
    modelFallback: '#A98C4E',
    modelColorOverrides: {
      claude: '#C2620A',
      openai: '#0F7A5C',
      gemini: '#2F6BD8',
      deepseek: '#3D57D6',
      qwen: '#7547D1',
      grok: '#5B6470',
      kimi: '#0E7C8C',
      zhipu: '#2F49CC',
      doubao: '#2E66C8',
      minimax: '#C4454B',
      hunyuan: '#0E8496',
      mistral: '#9A6700',
    },
    charRain: '#B2B598',
    charRainLead: '#5F7D47',
    nodeSurface: '#FFFDF8',
    nodeLabelSurface: '#F6F3EB',
    nodeLabelText: '#38372B',
    userAccent: '#7FA463',
    userCore: '#5F7D47',
    hubHalo: '#E4C276',
    hubSurface: '#FFFDF8',
    hubOrbitPrimary: '#D8984C',
    hubOrbitSecondary: '#7FA463',
    hubOrbitCore: '#A87643',
    hubRing: '#C08B57',
    hubLabel: '#38372B',
    channelStroke: '#A87643',
    channelGlow: '#D8984C',
    channelPacket: '#C08B57',
    channelCore: '#38372B',
    responsePacket: '#5F7D47',
    packetCore: '#38372B',
    accent: '#D8984C',
    glowComposite: 'source-over',
  },
}

type CanvasColorKey = Exclude<
  keyof CanvasTheme,
  'name' | 'modelColorOverrides' | 'glowComposite'
>

// 每个结构色对应的 CSS 令牌；解析失败时回退到 CANVAS_THEMES 的同名值。
// 日间只映射取值与回退色完全相同的令牌，保证像素不变。
const CANVAS_TOKENS: Record<CanvasThemeName, Record<CanvasColorKey, string>> = {
  dark: {
    backgroundTop: '--surface-container-lowest',
    backgroundBottom: '--page-background',
    mapLand: '--signal',
    mapHighlight: '--accent',
    mapOcean: '--signal-deep',
    mapLabel: '--text-secondary',
    mapFlow: '--glow-strong',
    mapRipple: '--glow',
    mapGlyph: '--signal-bright',
    mapGlyphAccent: '--accent',
    mapCursorGlow: '--accent',
    mapCursorRing: '--focus-ring',
    modelFallback: '--signal',
    charRain: '--signal-strong',
    charRainLead: '--glow',
    nodeSurface: '--surface-container',
    nodeLabelSurface: '--surface-container-lowest',
    nodeLabelText: '--text-primary',
    userAccent: '--glow',
    userCore: '--glow-bright',
    hubHalo: '--signal-strong',
    hubSurface: '--surface-container-lowest',
    hubOrbitPrimary: '--accent',
    hubOrbitSecondary: '--glow',
    hubOrbitCore: '--accent-hover',
    hubRing: '--accent',
    hubLabel: '--text-primary',
    channelStroke: '--accent',
    channelGlow: '--accent',
    channelPacket: '--accent-hover',
    channelCore: '--text-primary',
    responsePacket: '--glow',
    packetCore: '--on-colored',
    accent: '--accent',
  },
  light: {
    backgroundTop: '--surface-container-lowest',
    backgroundBottom: '--canvas-background-bottom',
    mapLand: '--signal',
    mapHighlight: '--accent',
    mapOcean: '--canvas-map-ocean',
    mapLabel: '--canvas-map-label',
    mapFlow: '--canvas-map-flow',
    mapRipple: '--glow',
    mapGlyph: '--signal-strong',
    mapGlyphAccent: '--accent-active',
    mapCursorGlow: '--accent',
    mapCursorRing: '--focus-ring',
    modelFallback: '--support-strong',
    charRain: '--canvas-char-rain',
    charRainLead: '--glow-strong',
    nodeSurface: '--surface-container',
    nodeLabelSurface: '--surface-container-lowest',
    nodeLabelText: '--text-primary',
    userAccent: '--glow',
    userCore: '--glow-strong',
    hubHalo: '--canvas-hub-halo',
    hubSurface: '--surface-container',
    hubOrbitPrimary: '--accent',
    hubOrbitSecondary: '--glow',
    hubOrbitCore: '--accent-active',
    hubRing: '--accent-hover',
    hubLabel: '--text-primary',
    channelStroke: '--accent-active',
    channelGlow: '--accent',
    channelPacket: '--accent-hover',
    channelCore: '--text-primary',
    responsePacket: '--glow-strong',
    packetCore: '--text-primary',
    accent: '--accent',
  },
}

export function getCanvasTheme(name: CanvasThemeName): CanvasTheme {
  const fallback = CANVAS_THEMES[name]
  if (typeof document === 'undefined') return fallback

  const style = getComputedStyle(document.documentElement)
  const theme = { ...fallback }
  const tokens = CANVAS_TOKENS[name]
  for (const key of Object.keys(tokens) as CanvasColorKey[]) {
    const value = style.getPropertyValue(tokens[key]).trim()
    theme[key] = normalizeOpaqueColor(value, fallback[key])
  }
  return theme
}

export function withAlpha(hex: string, alpha: number): string {
  const value = hex.replace('#', '')
  if (value.length !== 6) return hex
  return `rgba(${parseInt(value.slice(0, 2), 16)},${parseInt(value.slice(2, 4), 16)},${parseInt(value.slice(4, 6), 16)},${alpha})`
}
