/**
 * Palette used by the canvas scene. Keep this independent from CSS variables:
 * canvas pixels cannot inherit document tokens, and a single palette lets an
 * already-running scene change colour without rebuilding its animation state.
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
  dark: {
    name: 'dark',
    backgroundTop: '#0A1310',
    backgroundBottom: '#10251D',
    mapLand: '#B7D5C6',
    mapHighlight: '#E8C873',
    mapOcean: '#38705C',
    mapLabel: '#B0CFC0',
    mapFlow: '#70CAD8',
    mapRipple: '#78D7E8',
    mapGlyph: '#8CD2AF',
    mapGlyphAccent: '#E8C873',
    mapCursorGlow: '#D8A63A',
    mapCursorRing: '#E8C873',
    modelFallback: '#79B394',
    modelColorOverrides: {},
    charRain: '#3F8369',
    charRainLead: '#B0CFC0',
    nodeSurface: '#10251D',
    nodeLabelSurface: '#0A1310',
    nodeLabelText: '#EDF5F0',
    userAccent: '#D8A63A',
    userCore: '#F3DFA8',
    hubHalo: '#1F5A46',
    hubSurface: '#0A1310',
    hubOrbitPrimary: '#D8A63A',
    hubOrbitSecondary: '#79B394',
    hubOrbitCore: '#F3DFA8',
    hubRing: '#D8A63A',
    hubLabel: '#EDF5F0',
    channelStroke: '#D8A63A',
    channelGlow: '#D8A63A',
    channelPacket: '#F3DFA8',
    channelCore: '#FFFAE8',
    responsePacket: '#B0CFC0',
    packetCore: '#FFFFFF',
    accent: '#D8A63A',
    glowComposite: 'lighter',
  },
  light: {
    name: 'light',
    backgroundTop: '#F7FCFA',
    backgroundBottom: '#DFF4F3',
    mapLand: '#0B6B57',
    mapHighlight: '#878ECD',
    mapOcean: '#007F8B',
    mapLabel: '#45685E',
    mapFlow: '#0DCEDA',
    mapRipple: '#007F8B',
    mapGlyph: '#0B6B57',
    mapGlyphAccent: '#5D639E',
    mapCursorGlow: '#00E0FF',
    mapCursorRing: '#007F8B',
    modelFallback: '#5D639E',
    modelColorOverrides: {
      openai: '#087A69',
      grok: '#5B6470',
      kimi: '#007F8B',
      minimax: '#C44553',
      hunyuan: '#087B87',
      mistral: '#9A6700',
    },
    charRain: '#0B6B57',
    charRainLead: '#0DCEDA',
    nodeSurface: '#FFFFFF',
    nodeLabelSurface: '#F7FCFA',
    nodeLabelText: '#102A24',
    userAccent: '#0B6B57',
    userCore: '#6EF3D6',
    hubHalo: '#6EF3D6',
    hubSurface: '#FFFFFF',
    hubOrbitPrimary: '#0DCEDA',
    hubOrbitSecondary: '#878ECD',
    hubOrbitCore: '#0B6B57',
    hubRing: '#0B6B57',
    hubLabel: '#102A24',
    channelStroke: '#0B6B57',
    channelGlow: '#00E0FF',
    channelPacket: '#0DCEDA',
    channelCore: '#102A24',
    responsePacket: '#5D639E',
    packetCore: '#102A24',
    accent: '#0B6B57',
    glowComposite: 'source-over',
  },
}

export function getCanvasTheme(name: CanvasThemeName): CanvasTheme {
  return CANVAS_THEMES[name]
}

export function withAlpha(hex: string, alpha: number): string {
  const value = hex.replace('#', '')
  if (value.length !== 6) return hex
  return `rgba(${parseInt(value.slice(0, 2), 16)},${parseInt(value.slice(2, 4), 16)},${parseInt(value.slice(4, 6), 16)},${alpha})`
}
